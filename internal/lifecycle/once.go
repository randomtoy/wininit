package lifecycle

import (
	"context"
	"errors"
	"time"

	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/state"
)

func (e *Engine) executeOnce(ctx context.Context, current *state.State, provisioner plugin.OnceProvisioner, metadata plugin.Metadata) (plugin.Result, error) {
	key, approval, err := provisioner.ExecutionIdentity(metadata)
	if err != nil {
		return plugin.Result{}, err
	}
	if !state.ValidHash(key) || !state.ValidHash(approval) {
		return plugin.Result{}, errors.New("invalid one-shot execution identity")
	}
	if previous, ok := current.Executions[key]; ok {
		if previous.ApprovalHash != approval {
			return plugin.Result{}, errors.New("execution_id was already used for a different approved script; choose a new execution_id")
		}
		switch previous.Status {
		case "succeeded":
			return plugin.Result{Reused: true}, nil
		case "reboot_required":
			boot, err := e.bootID(ctx)
			if err != nil {
				return plugin.Result{}, err
			}
			if !state.ValidHash(boot) {
				return plugin.Result{}, errors.New("invalid current boot identity")
			}
			return plugin.Result{Reused: true, RebootRequired: boot == previous.BootID}, nil
		case "started":
			previous.Status, previous.FailureCode, previous.FinishedAt = "interrupted", "outcome_unknown", e.now().UTC()
			current.Executions[key] = previous
			if err := e.save(ctx, current); err != nil {
				return plugin.Result{}, err
			}
		}
		return plugin.Result{}, errors.New("previous script execution failed or has an unknown outcome; choose a new execution_id to authorize another attempt")
	}
	if len(current.Executions) >= state.MaxExecutions {
		return plugin.Result{}, errors.New("execution journal is full; no history is automatically evicted")
	}
	prepared, err := provisioner.Prepare(ctx, metadata)
	if err != nil {
		return plugin.Result{}, err
	}
	if prepared.Close != nil {
		defer prepared.Close()
	}
	if prepared.Skipped {
		return plugin.Result{Skipped: true}, nil
	}
	if prepared.Execute == nil {
		return plugin.Result{}, errors.New("prepared execution has no execute operation")
	}
	boot, err := e.bootID(ctx)
	if err != nil {
		return plugin.Result{}, err
	}
	if !state.ValidHash(boot) {
		return plugin.Result{}, errors.New("invalid current boot identity")
	}
	if current.Executions == nil {
		current.Executions = make(map[string]state.Execution)
	}
	record := state.Execution{ApprovalHash: approval, BootID: boot, Status: "started", StartedAt: e.now().UTC()}
	current.Executions[key] = record
	if err := e.save(ctx, current); err != nil {
		delete(current.Executions, key) // Execute was never called.
		return plugin.Result{}, err
	}
	result, executionErr := prepared.Execute(ctx)
	record.FinishedAt = e.now().UTC()
	record.HasExitCode, record.ExitCode, record.OutputBytes = result.HasExitCode, result.ExitCode, result.OutputBytes
	record.Status = "succeeded"
	if result.RebootRequired {
		record.Status = "reboot_required"
	}
	if executionErr != nil {
		record.Status, record.FailureCode = "failed", result.FailureCode
		if record.FailureCode == "" {
			record.FailureCode = "plugin_failed"
		}
		if ctx.Err() != nil && (errors.Is(executionErr, context.Canceled) || errors.Is(executionErr, context.DeadlineExceeded)) {
			record.Status, record.FailureCode = "interrupted", "cancelled"
		}
	}
	current.Executions[key] = record
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := e.save(finishCtx, current); err != nil {
		return plugin.Result{}, errors.Join(executionErr, err)
	}
	return result.Result, executionErr
}
