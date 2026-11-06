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
	datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Settings struct {
	Executable     string `yaml:"executable" json:"executable"`
	SHA256         string `yaml:"sha256" json:"sha256"`
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"`
}

func (s Settings) Validate() error {
	digest, err := hex.DecodeString(s.SHA256)
	if !identity.LocalAbsolutePath(s.Executable) || err != nil || len(digest) != sha256.Size || s.TimeoutSeconds < 1 || s.TimeoutSeconds > 60 {
		return errors.New("external ConfigDrive requires an absolute local executable, exact SHA-256, and timeout_seconds 1..60")
	}
	return nil
}

type ConfigDrive struct {
	settings  Settings
	directory string
	discover  bool
}

func NewConfigDrive(settings Settings, directory string, discover bool) *ConfigDrive {
	return &ConfigDrive{settings, directory, discover}
}
func (*ConfigDrive) Name() string { return "configdrive" }

func (p *ConfigDrive) Discover(parent context.Context) (plugin.Metadata, error) {
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
		return plugin.Metadata{}, errors.New("external ConfigDrive executable security check failed")
	}
	defer executable.Close() // Windows keeps the verified image locked against write/delete.
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(executable, (128<<20)+1)); err != nil {
		return plugin.Metadata{}, errors.New("read external ConfigDrive executable failed")
	}
	info, err := executable.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return plugin.Metadata{}, errors.New("external ConfigDrive executable is not a bounded regular file")
	}
	digest, _ := hex.DecodeString(p.settings.SHA256)
	if subtle.ConstantTimeCompare(hash.Sum(nil), digest) != 1 {
		return plugin.Metadata{}, errors.New("external ConfigDrive SHA-256 mismatch")
	}
	if err := ctx.Err(); err != nil {
		return plugin.Metadata{}, err
	}
	var process *processRunner
	var startupFailure error
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  datasourcev1.Handshake(),
		Plugins:          goplugin.PluginSet{datasourcev1.PluginKey: &datasourcev1.GRPCPlugin{}},
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		AutoMTLS:         true, SkipHostEnv: true, StartTimeout: time.Until(deadline(ctx)),
		GRPCDialOptions: []grpc.DialOption{
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(datasourcev1.MaxMessageBytes), grpc.MaxCallSendMsgSize(datasourcev1.MaxMessageBytes)),
			grpc.WithUnaryInterceptor(func(callCtx context.Context, method string, request, response interface{}, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
				bounded, cancel := context.WithDeadline(callCtx, deadline(ctx))
				stop := context.AfterFunc(ctx, cancel)
				defer cancel()
				defer stop()
				return invoke(bounded, method, request, response, connection, options...)
			}),
		},
		Logger: hclog.NewNullLogger(), SyncStdout: io.Discard, SyncStderr: io.Discard,
		RunnerFunc: func(_ hclog.Logger, spec *exec.Cmd, directory string) (runner.Runner, error) {
			cmd := exec.CommandContext(ctx, p.settings.Executable)
			cmd.Env = append(environment(), spec.Env...)
			var err error
			process, err = newProcessRunner(cmd, directory)
			if err != nil {
				startupFailure = processError("credential/job preparation", err)
			}
			return process, err
		},
	})
	defer client.Kill()
	connection, err := client.Client()
	if err != nil {
		if process != nil && process.cancelled.Load() && ctx.Err() != nil {
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
				return plugin.Metadata{}, fmt.Errorf("external ConfigDrive exited before protocol handshake (exit code 0x%x)", uint32(status))
			}
		}
		return plugin.Metadata{}, errors.New("external ConfigDrive startup or protocol handshake failed")
	}
	raw, err := connection.Dispense(datasourcev1.PluginKey)
	if err != nil {
		return plugin.Metadata{}, errors.New("external ConfigDrive interface negotiation failed")
	}
	rpc, ok := raw.(datasourcev1.DatasourceClient)
	if !ok {
		return plugin.Metadata{}, errors.New("external ConfigDrive returned an incompatible interface")
	}
	description, err := rpc.Describe(ctx, &datasourcev1.DescribeRequest{})
	if err != nil {
		if ctx.Err() != nil && (status.Code(err) == codes.Canceled || status.Code(err) == codes.DeadlineExceeded) {
			return plugin.Metadata{}, ctx.Err()
		}
		return plugin.Metadata{}, errors.New("external ConfigDrive identity negotiation failed")
	}
	if description.GetProtocolVersion() != datasourcev1.ProtocolVersion || description.GetName() != datasourcev1.PluginKey || description.GetKind() != "datasource" {
		return plugin.Metadata{}, errors.New("external ConfigDrive identity or protocol version mismatch")
	}
	response, err := rpc.Discover(ctx, &datasourcev1.DiscoverRequest{SeedDirectory: p.directory, DiscoverVolumes: p.discover})
	if err != nil {
		if ctx.Err() != nil && (status.Code(err) == codes.Canceled || status.Code(err) == codes.DeadlineExceeded) {
			return plugin.Metadata{}, ctx.Err()
		}
		return plugin.Metadata{}, errors.New("external ConfigDrive discovery failed")
	}
	if response.GetUnavailable() {
		if response.GetMetadata() != nil {
			return plugin.Metadata{}, errors.New("external ConfigDrive returned conflicting availability")
		}
		return plugin.Metadata{}, plugin.ErrUnavailable
	}
	if response.GetMetadata() == nil {
		return plugin.Metadata{}, errors.New("external ConfigDrive returned no metadata")
	}
	// Validate the untrusted response against the same bounded input contract as
	// built-in ConfigDrive. No external status/error text reaches logs or state.
	data, _ := json.Marshal(map[string]string{"uuid": response.GetMetadata().GetInstanceId(), "hostname": response.GetMetadata().GetLocalHostname()})
	metadata, err := configdrive.Parse(data)
	if err != nil {
		return plugin.Metadata{}, err
	}
	if metadata.InstanceID != response.GetMetadata().GetInstanceId() || metadata.LocalHostname != response.GetMetadata().GetLocalHostname() {
		return plugin.Metadata{}, errors.New("external ConfigDrive response is not normalized")
	}
	return metadata, nil
}
func deadline(ctx context.Context) time.Time { value, _ := ctx.Deadline(); return value }
