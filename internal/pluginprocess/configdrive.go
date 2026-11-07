// Package pluginprocess hosts verified, explicitly configured executable plugins.
package pluginprocess

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/go-plugin/runner"
	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/plugins/datasource/configdrive"
	"github.com/randomtoy/wininit/plugins/datasource/nocloud"
	datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"
)

type Settings struct {
	Executable     string `yaml:"executable" json:"executable"`
	SHA256         string `yaml:"sha256" json:"sha256"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
}

func (s Settings) Validate() error {
	digest, err := hex.DecodeString(s.SHA256)
	if !identity.LocalAbsolutePath(s.Executable) || err != nil || len(digest) != sha256.Size || s.TimeoutSeconds < 1 || s.TimeoutSeconds > 60 {
		return errors.New("external datasource requires an absolute local executable, exact SHA-256, and timeout_seconds 1..60")
	}
	return nil
}

type Datasource struct {
	name      string
	settings  Settings
	directory string
	discover  bool
}

func NewConfigDrive(settings Settings, directory string, discover bool) *ConfigDrive {
	return &Datasource{name: "configdrive", settings: settings, directory: directory, discover: discover}
}

// ConfigDrive preserves the original public-internal adapter type.
type ConfigDrive = Datasource

func NewNoCloud(settings Settings, directory string, discover bool) *Datasource {
	return &Datasource{name: "nocloud", settings: settings, directory: directory, discover: discover}
}
func (p *Datasource) Name() string { return p.name }
func (p *Datasource) label() string {
	if p.name == "nocloud" {
		return "NoCloud"
	}
	return "ConfigDrive"
}

func (p *Datasource) Discover(parent context.Context) (plugin.Metadata, error) {
	if err := parent.Err(); err != nil {
		return plugin.Metadata{}, err
	}
	if err := p.settings.Validate(); err != nil {
		return plugin.Metadata{}, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(p.settings.TimeoutSeconds)*time.Second)
	defer cancel()
	executable, err := openExecutable(p.settings.Executable)
	if err != nil {
		return plugin.Metadata{}, fmt.Errorf("external %s executable security check failed", p.label())
	}
	defer executable.Close() // Windows keeps the verified image locked against write/delete.
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(executable, (128<<20)+1)); err != nil {
		return plugin.Metadata{}, fmt.Errorf("read external %s executable failed", p.label())
	}
	info, err := executable.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return plugin.Metadata{}, fmt.Errorf("external %s executable is not a bounded regular file", p.label())
	}
	digest, _ := hex.DecodeString(p.settings.SHA256)
	if subtle.ConstantTimeCompare(hash.Sum(nil), digest) != 1 {
		return plugin.Metadata{}, fmt.Errorf("external %s SHA-256 mismatch", p.label())
	}
	if err := ctx.Err(); err != nil {
		return plugin.Metadata{}, err
	}
	var process *processRunner
	var startupFailure error
	startupCancelled := false
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  datasourcev1.Handshake(),
		Plugins:          goplugin.PluginSet{p.name: &datasourcev1.GRPCPlugin{}},
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		AutoMTLS:         true, SkipHostEnv: true, StartTimeout: time.Until(deadline(ctx)),
		GRPCDialOptions: []grpc.DialOption{
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(datasourcev1.MaxMessageBytes), grpc.MaxCallSendMsgSize(datasourcev1.MaxMessageBytes)),
			grpc.WithUnaryInterceptor(func(callCtx context.Context, method string, request, response interface{}, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
				// Do not add an independent timer at an equal/earlier parent
				// deadline: it could fire before the parent publishes Err().
				var bounded context.Context
				var cancel context.CancelFunc
				if existing, ok := callCtx.Deadline(); ok && !existing.After(deadline(ctx)) {
					bounded, cancel = context.WithCancel(callCtx)
				} else {
					bounded, cancel = context.WithDeadline(callCtx, deadline(ctx))
				}
				stop := context.AfterFunc(ctx, cancel)
				defer cancel()
				defer stop()
				// The host owns the deadline timer. Forward cancellation without
				// grpc-timeout: a rounded remote timer could expire first and
				// misclassify host cancellation as a provider failure.
				return invoke(cancellationContext{bounded}, method, request, response, connection, options...)
			}),
		},
		Logger: hclog.NewNullLogger(), SyncStdout: io.Discard, SyncStderr: io.Discard,
		RunnerFunc: func(_ hclog.Logger, spec *exec.Cmd, directory string) (runner.Runner, error) {
			cmd := exec.CommandContext(ctx, p.settings.Executable)
			cmd.Env = append(environment(), spec.Env...)
			var err error
			process, err = newProcessRunnerProfile(ctx, cmd, directory, roleDatasource, p.label())
			if err != nil {
				startupCancelled = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
				startupFailure = processErrorFor(p.label(), "credential/job preparation", err)
			}
			return process, err
		},
	})
	defer client.Kill()
	connection, err := client.Client()
	if err != nil {
		if ctx.Err() != nil && (startupCancelled || process != nil && (process.cancelled.Load() || process.startCancelled)) {
			return plugin.Metadata{}, ctx.Err()
		}
		if startupFailure != nil {
			return plugin.Metadata{}, startupFailure
		}
		if process != nil && process.startFailure != nil {
			return plugin.Metadata{}, process.startFailure
		}
		if process != nil {
			// stdout can close just before os/exec publishes the exit status.
			// Wait briefly for the numeric code, never for raw child output.
			select {
			case <-process.finished:
			case <-time.After(100 * time.Millisecond):
			}
			if status := process.exitStatus.Load(); status != 0 {
				return plugin.Metadata{}, fmt.Errorf("external %s exited before protocol handshake (exit code 0x%x)", p.label(), uint32(status))
			}
		}
		return plugin.Metadata{}, fmt.Errorf("external %s startup or protocol handshake failed", p.label())
	}
	raw, err := connection.Dispense(p.name)
	if err != nil {
		if cancelledRPC(ctx, process, err) {
			return plugin.Metadata{}, ctx.Err()
		}
		return plugin.Metadata{}, fmt.Errorf("external %s interface negotiation failed", p.label())
	}
	rpc, ok := raw.(datasourcev1.DatasourceClient)
	if !ok {
		return plugin.Metadata{}, fmt.Errorf("external %s returned an incompatible interface", p.label())
	}
	description, err := rpc.Describe(ctx, &datasourcev1.DescribeRequest{})
	if err != nil {
		if cancelledRPC(ctx, process, err) {
			return plugin.Metadata{}, ctx.Err()
		}
		return plugin.Metadata{}, fmt.Errorf("external %s identity negotiation failed", p.label())
	}
	if description.GetProtocolVersion() != datasourcev1.ProtocolVersion || description.GetName() != p.name || description.GetKind() != "datasource" {
		return plugin.Metadata{}, fmt.Errorf("external %s identity or protocol version mismatch", p.label())
	}
	response, err := rpc.Discover(ctx, &datasourcev1.DiscoverRequest{SeedDirectory: p.directory, DiscoverVolumes: p.discover})
	if err != nil {
		if cancelledRPC(ctx, process, err) {
			return plugin.Metadata{}, ctx.Err()
		}
		return plugin.Metadata{}, fmt.Errorf("external %s discovery failed (RPC code %s)", p.label(), status.Code(err))
	}
	if response.GetUnavailable() {
		if response.GetMetadata() != nil {
			return plugin.Metadata{}, fmt.Errorf("external %s returned conflicting availability", p.label())
		}
		return plugin.Metadata{}, plugin.ErrUnavailable
	}
	if response.GetMetadata() == nil {
		return plugin.Metadata{}, fmt.Errorf("external %s returned no metadata", p.label())
	}
	// Validate the untrusted response against the same bounded input contract as
	// built-in ConfigDrive. No external status/error text reaches logs or state.
	data, _ := json.Marshal(map[string]string{"uuid": response.GetMetadata().GetInstanceId(), "hostname": response.GetMetadata().GetLocalHostname()})
	metadata, err := configdrive.Parse(data)
	if p.name == "nocloud" {
		data, _ = yaml.Marshal(map[string]string{"instance-id": response.GetMetadata().GetInstanceId(), "local-hostname": response.GetMetadata().GetLocalHostname()})
		metadata, err = nocloud.Parse(data)
	}
	if err != nil {
		return plugin.Metadata{}, err
	}
	if metadata.InstanceID != response.GetMetadata().GetInstanceId() || metadata.LocalHostname != response.GetMetadata().GetLocalHostname() {
		return plugin.Metadata{}, fmt.Errorf("external %s response is not normalized", p.label())
	}
	return metadata, nil
}
func deadline(ctx context.Context) time.Time { value, _ := ctx.Deadline(); return value }

// Keep Done/Err from the bounded host context, without installing another
// deadline timer in gRPC or the remote process. The SDK also bounds providers.
type cancellationContext struct{ context.Context }

func (cancellationContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func cancelledRPC(ctx context.Context, process *processRunner, cause error) bool {
	if ctx.Err() == nil {
		return false
	}
	switch status.Code(cause) {
	case codes.Canceled, codes.DeadlineExceeded:
		return true
	case codes.Unavailable:
		return process != nil && process.cancelled.Load()
	}
	return false
}
