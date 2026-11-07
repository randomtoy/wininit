package pluginprocess

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/go-plugin/runner"
	"github.com/randomtoy/wininit/internal/plugin"
	provisionerv1 "github.com/randomtoy/wininit/sdk/provisioner/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"io"
	"os/exec"
	"time"
)

type ProvisionerSettings struct {
	AllowPrivileged bool     `yaml:"allow_privileged" json:"allow_privileged"`
	Process         Settings `yaml:"process" json:"process"`
}

func (s ProvisionerSettings) Validate() error {
	if !s.AllowPrivileged {
		return errors.New("external hostname requires allow_privileged: true")
	}
	if err := s.Process.Validate(); err != nil {
		return errors.New("external hostname requires an absolute local executable, exact SHA-256, and timeout_seconds 1..60")
	}
	return nil
}

var errProvisionerTimeout = errors.New("external provisioner session deadline exceeded")

func provisionerContextError(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), errProvisionerTimeout) {
		return errProvisionerTimeout
	}
	return ctx.Err()
}

type Provisioner struct {
	name     string
	settings ProvisionerSettings
}

func NewHostname(settings ProvisionerSettings) *Provisioner {
	return &Provisioner{name: "hostname", settings: settings}
}
func (p *Provisioner) Name() string  { return p.name }
func (p *Provisioner) label() string { return "hostname" }
func (p *Provisioner) Apply(parent context.Context, metadata plugin.Metadata) (plugin.Result, error) {
	if err := parent.Err(); err != nil {
		return plugin.Result{}, err
	}
	if err := p.settings.Validate(); err != nil {
		return plugin.Result{}, err
	}
	input := &provisionerv1.Metadata{InstanceId: metadata.InstanceID, LocalHostname: metadata.LocalHostname, Datasource: metadata.Datasource}
	if err := provisionerv1.ValidateMetadata(input); err != nil {
		return plugin.Result{}, err
	}
	ctx, cancel := context.WithTimeoutCause(parent, time.Duration(p.settings.Process.TimeoutSeconds)*time.Second, errProvisionerTimeout)
	defer cancel()
	executable, err := openExecutable(p.settings.Process.Executable)
	if err != nil {
		return plugin.Result{}, fmt.Errorf("external %s executable security check failed", p.label())
	}
	defer executable.Close() // Windows keeps the verified image locked against write/delete.
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(executable, (128<<20)+1)); err != nil {
		return plugin.Result{}, fmt.Errorf("read external %s executable failed", p.label())
	}
	info, err := executable.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return plugin.Result{}, fmt.Errorf("external %s executable is not a bounded regular file", p.label())
	}
	digest, _ := hex.DecodeString(p.settings.Process.SHA256)
	if subtle.ConstantTimeCompare(hash.Sum(nil), digest) != 1 {
		return plugin.Result{}, fmt.Errorf("external %s SHA-256 mismatch", p.label())
	}
	if err := ctx.Err(); err != nil {
		return plugin.Result{}, provisionerContextError(ctx)
	}
	var process *processRunner
	var startupFailure error
	startupCancelled := false
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  provisionerv1.Handshake(),
		Plugins:          goplugin.PluginSet{provisionerv1.PluginKey: &provisionerv1.GRPCPlugin{}},
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		AutoMTLS:         true, SkipHostEnv: true, StartTimeout: time.Until(deadline(ctx)),
		GRPCDialOptions: []grpc.DialOption{
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(provisionerv1.MaxMessageBytes), grpc.MaxCallSendMsgSize(provisionerv1.MaxMessageBytes)),
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
			cmd := exec.CommandContext(ctx, p.settings.Process.Executable)
			cmd.Env = append(environment(), spec.Env...)
			var err error
			process, err = newProcessRunnerProfile(ctx, cmd, directory, roleProvisioner, p.label())
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
			return plugin.Result{}, provisionerContextError(ctx)
		}
		if startupFailure != nil {
			return plugin.Result{}, startupFailure
		}
		if process != nil && process.startFailure != nil {
			return plugin.Result{}, process.startFailure
		}
		if process != nil {
			// stdout can close just before os/exec publishes the exit status.
			// Wait briefly for the numeric code, never for raw child output.
			select {
			case <-process.finished:
			case <-time.After(100 * time.Millisecond):
			}
			if status := process.exitStatus.Load(); status != 0 {
				return plugin.Result{}, fmt.Errorf("external %s exited before protocol handshake (exit code 0x%x)", p.label(), uint32(status))
			}
		}
		return plugin.Result{}, fmt.Errorf("external %s startup or protocol handshake failed", p.label())
	}
	raw, err := connection.Dispense(provisionerv1.PluginKey)
	if err != nil {
		if cancelledRPC(ctx, process, err) {
			return plugin.Result{}, provisionerContextError(ctx)
		}
		return plugin.Result{}, fmt.Errorf("external %s interface negotiation failed", p.label())
	}
	rpc, ok := raw.(provisionerv1.ProvisionerClient)
	if !ok {
		return plugin.Result{}, fmt.Errorf("external %s returned an incompatible interface", p.label())
	}
	description, err := rpc.Describe(ctx, &provisionerv1.DescribeRequest{})
	if err != nil {
		if cancelledRPC(ctx, process, err) {
			return plugin.Result{}, provisionerContextError(ctx)
		}
		return plugin.Result{}, fmt.Errorf("external %s identity negotiation failed", p.label())
	}
	if description.GetProtocolVersion() != provisionerv1.ProtocolVersion || description.GetName() != p.name || description.GetKind() != "provisioner" || !description.GetIdempotent() {
		return plugin.Result{}, fmt.Errorf("external %s identity or protocol version mismatch", p.label())
	}
	response, err := rpc.Apply(ctx, &provisionerv1.ApplyRequest{Metadata: input})
	if err != nil {
		if cancelledRPC(ctx, process, err) {
			return plugin.Result{}, provisionerContextError(ctx)
		}
		return plugin.Result{}, fmt.Errorf("external %s reconciliation failed (RPC code %s)", p.label(), status.Code(err))
	}

	switch response.GetOutcome() {
	case provisionerv1.Outcome_UNCHANGED:
		return plugin.Result{}, nil
	case provisionerv1.Outcome_CHANGED:
		return plugin.Result{Changed: true}, nil
	case provisionerv1.Outcome_REBOOT_REQUIRED:
		return plugin.Result{RebootRequired: true}, nil
	case provisionerv1.Outcome_CHANGED_REBOOT_REQUIRED:
		return plugin.Result{Changed: true, RebootRequired: true}, nil
	case provisionerv1.Outcome_SKIPPED:
		return plugin.Result{Skipped: true}, nil
	default:
		return plugin.Result{}, errors.New("external hostname returned an invalid outcome")
	}
}
