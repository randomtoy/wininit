package pluginprocess

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"github.com/hashicorp/go-plugin/runner"
	"github.com/randomtoy/wininit/internal/plugin"
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

var errPowerShellSessionTimeout = errors.New("PowerShell plugin session deadline exceeded")

var _ plugin.OnceProvisioner = (*PowerShell)(nil)

func (*PowerShell) Apply(context.Context, plugin.Metadata) (plugin.Result, error) {
	return plugin.Result{}, errors.New("PowerShell user-data requires the lifecycle execution journal")
}

func (p *PowerShell) Prepare(parent context.Context, metadata plugin.Metadata) (prepared plugin.PreparedExecution, err error) {
	if !p.settings.Enabled {
		return prepared, errors.New("PowerShell user-data is disabled")
	}
	if err = p.settings.Validate(); err != nil {
		return prepared, err
	}
	if err = parent.Err(); err != nil {
		return prepared, err
	}
	source, ok := p.sources[metadata.Datasource]
	if !ok || (metadata.Datasource != "nocloud" && metadata.Datasource != "configdrive") {
		return prepared, errors.New("PowerShell user-data requires the host-selected NoCloud or ConfigDrive source")
	}
	ctx, cancel := context.WithTimeoutCause(parent, time.Duration(p.settings.Process.TimeoutSeconds)*time.Second, errPowerShellSessionTimeout)
	var client *goplugin.Client
	var image io.Closer
	closeSession := func() {
		cancel() // Kill the entire job before waiting for framework shutdown.
		if client != nil {
			client.Kill()
		}
		if image != nil {
			image.Close()
		}
	}
	defer func() {
		if err != nil || prepared.Skipped {
			closeSession()
		}
	}()
	executable, openErr := openExecutable(p.settings.Process.Executable)
	if openErr != nil {
		return prepared, errors.New("PowerShell plugin executable security check failed")
	}
	image = executable
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(executable, (128<<20)+1)); err != nil {
		return prepared, errors.New("read PowerShell plugin executable failed")
	}
	info, statErr := executable.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return prepared, errors.New("PowerShell plugin executable must be a bounded regular file")
	}
	expected, _ := hex.DecodeString(p.settings.Process.SHA256)
	if subtle.ConstantTimeCompare(expected, hash.Sum(nil)) != 1 {
		return prepared, errors.New("PowerShell plugin SHA-256 mismatch")
	}
	if err = ctx.Err(); err != nil {
		return prepared, powerShellContextError(ctx)
	}
	var process *processRunner
	var startupFailure error
	startupCancelled := false
	startup := time.Until(deadline(ctx))
	if startup > 15*time.Second {
		startup = 15 * time.Second
	}
	client = goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig: userdatav1.Handshake(), Plugins: goplugin.PluginSet{userdatav1.PluginKey: &userdatav1.GRPCPlugin{}}, AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC}, AutoMTLS: true, SkipHostEnv: true, StartTimeout: startup,
		GRPCDialOptions: []grpc.DialOption{grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(userdatav1.MaxMessageBytes), grpc.MaxCallSendMsgSize(userdatav1.MaxMessageBytes)), grpc.WithUnaryInterceptor(func(callCtx context.Context, method string, request, response interface{}, connection *grpc.ClientConn, invoke grpc.UnaryInvoker, options ...grpc.CallOption) error {
			var bounded context.Context
			var stopBound context.CancelFunc
			if existing, ok := callCtx.Deadline(); ok && !existing.After(deadline(ctx)) {
				bounded, stopBound = context.WithCancel(callCtx)
			} else {
				bounded, stopBound = context.WithDeadline(callCtx, deadline(ctx))
			}
			stop := context.AfterFunc(ctx, stopBound)
			defer stop()
			defer stopBound()
			return invoke(cancellationContext{bounded}, method, request, response, connection, options...)
		})}, Logger: hclog.NewNullLogger(), SyncStdout: io.Discard, SyncStderr: io.Discard,
		RunnerFunc: func(_ hclog.Logger, spec *exec.Cmd, directory string) (runner.Runner, error) {
			cmd := exec.CommandContext(ctx, p.settings.Process.Executable)
			cmd.Env = append(environment(), spec.Env...)
			var startErr error
			process, startErr = newProcessRunnerProfile(ctx, cmd, directory, true, "PowerShell user-data")
			if startErr != nil {
				startupCancelled = errors.Is(startErr, context.Canceled) || errors.Is(startErr, context.DeadlineExceeded)
				startupFailure = processErrorFor("PowerShell user-data", "credential/job preparation", startErr)
			}
			return process, startErr
		},
	})
	connection, clientErr := client.Client()
	if clientErr != nil {
		if ctx.Err() != nil && (startupCancelled || process != nil && (process.cancelled.Load() || process.startCancelled)) {
			return prepared, powerShellContextError(ctx)
		}
		if startupFailure != nil {
			return prepared, startupFailure
		}
		if process != nil && process.startFailure != nil {
			return prepared, process.startFailure
		}
		return prepared, errors.New("PowerShell plugin startup or protocol handshake failed")
	}
	raw, dispenseErr := connection.Dispense(userdatav1.PluginKey)
	if dispenseErr != nil {
		return prepared, powerShellRPCError(ctx, process, dispenseErr, "interface negotiation")
	}
	rpc, ok := raw.(userdatav1.UserDataClient)
	if !ok {
		return prepared, errors.New("PowerShell plugin returned an incompatible interface")
	}
	description, describeErr := rpc.Describe(ctx, &userdatav1.DescribeRequest{})
	if describeErr != nil {
		return prepared, powerShellRPCError(ctx, process, describeErr, "identity negotiation")
	}
	if description.GetName() != userdatav1.PluginKey || description.GetKind() != "provisioner" || description.GetProtocolVersion() != userdatav1.ProtocolVersion {
		return prepared, errors.New("PowerShell plugin identity or protocol version mismatch")
	}
	response, prepareErr := rpc.Prepare(ctx, &userdatav1.PrepareRequest{Datasource: metadata.Datasource, SeedDirectory: source.Directory, DiscoverVolumes: source.DiscoverVolumes, ExpectedInstanceId: metadata.InstanceID, ExpectedHostname: metadata.LocalHostname, ApprovedSha256: strings.ToLower(p.settings.ScriptSHA256)})
	if prepareErr != nil {
		return prepared, powerShellRPCError(ctx, process, prepareErr, "preparation")
	}
	if response.GetUnavailable() {
		if response.GetLeaseId() != "" || response.GetSha256() != "" || response.GetSize() != 0 {
			return prepared, errors.New("PowerShell plugin returned conflicting preparation availability")
		}
		return plugin.PreparedExecution{Skipped: true}, nil
	}
	lease, leaseErr := hex.DecodeString(response.GetLeaseId())
	if leaseErr != nil || len(lease) != 16 || response.GetSize() == 0 || response.GetSize() > userdatav1.MaxScriptBytes || response.GetSha256() != strings.ToLower(p.settings.ScriptSHA256) {
		return prepared, errors.New("PowerShell plugin returned an invalid prepared script identity")
	}
	prepared.Close = closeSession
	prepared.Execute = func(callCtx context.Context) (plugin.ExecutionResult, error) {
		// The host has durably committed intent before invoking this closure.
		stop := context.AfterFunc(callCtx, cancel)
		defer stop()
		result, executeErr := rpc.Execute(ctx, &userdatav1.ExecuteRequest{LeaseId: response.GetLeaseId(), TimeoutSeconds: uint32(p.settings.Process.TimeoutSeconds), MaxOutputBytes: uint32(p.settings.MaxOutputBytes)})
		if executeErr != nil {
			failure := plugin.ExecutionResult{FailureCode: "plugin_failed"}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				failure.FailureCode = "timeout"
			}
			return failure, powerShellRPCError(ctx, process, executeErr, "execution")
		}
		value := plugin.ExecutionResult{HasExitCode: true, ExitCode: result.GetExitCode(), OutputBytes: result.GetOutputBytes()}
		if value.OutputBytes > uint64(p.settings.MaxOutputBytes)+1 || (result.GetOutcome() != userdatav1.Outcome_OUTPUT_LIMIT && value.OutputBytes > uint64(p.settings.MaxOutputBytes)) {
			return plugin.ExecutionResult{FailureCode: "protocol_error"}, errors.New("PowerShell plugin returned invalid output accounting")
		}
		switch result.GetOutcome() {
		case userdatav1.Outcome_SUCCESS:
			if value.ExitCode != 0 {
				return plugin.ExecutionResult{FailureCode: "protocol_error"}, errors.New("PowerShell plugin returned conflicting success")
			}
			value.Changed = true
			return value, nil
		case userdatav1.Outcome_REBOOT_REQUIRED:
			if value.ExitCode != 3010 {
				return plugin.ExecutionResult{FailureCode: "protocol_error"}, errors.New("PowerShell plugin returned conflicting reboot result")
			}
			value.Changed, value.RebootRequired = true, true
			return value, nil
		case userdatav1.Outcome_EXIT_FAILURE:
			if value.ExitCode == 0 || value.ExitCode == 3010 {
				return plugin.ExecutionResult{FailureCode: "protocol_error"}, errors.New("PowerShell plugin returned conflicting failure")
			}
			value.FailureCode = "exit_failure"
			return value, fmt.Errorf("PowerShell user-data failed (exit code %d)", value.ExitCode)
		case userdatav1.Outcome_OUTPUT_LIMIT:
			if value.OutputBytes != uint64(p.settings.MaxOutputBytes)+1 {
				return plugin.ExecutionResult{FailureCode: "protocol_error"}, errors.New("PowerShell plugin returned conflicting output limit")
			}
			value.FailureCode = "output_limit"
			return value, errors.New("PowerShell user-data exceeded the output limit; output was discarded")
		default:
			return plugin.ExecutionResult{FailureCode: "protocol_error"}, errors.New("PowerShell plugin returned an unknown outcome")
		}
	}
	return prepared, nil
}
func powerShellRPCError(ctx context.Context, process *processRunner, cause error, operation string) error {
	if cancelledRPC(ctx, process, cause) {
		return powerShellContextError(ctx)
	}
	return fmt.Errorf("PowerShell plugin %s failed (RPC code %s)", operation, status.Code(cause))
}

// A private session deadline must not masquerade as the root lifecycle deadline.
func powerShellContextError(ctx context.Context) error {
	if errors.Is(context.Cause(ctx), errPowerShellSessionTimeout) {
		return errPowerShellSessionTimeout
	}
	return ctx.Err()
}
