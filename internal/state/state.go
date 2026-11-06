// Package state defines persistent lifecycle state and storage contracts.
package state

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const CurrentSchemaVersion = 3
const MaxExecutions = 128

var (
	ErrUnsafePermissions = errors.New("unsafe Registry state permissions; protect the dedicated key and its ancestors before retrying")
	ErrNotFound          = errors.New("state not found")
	ErrUnsupportedSchema = errors.New("unsupported state schema")
	ErrCorrupt           = errors.New("corrupt state")
	ErrUnsupported       = errors.New("state store is unsupported on this platform")
)

type Phase string

const (
	PhaseNew             Phase = "new"
	PhaseAgentRunning    Phase = "agent_running"
	PhaseAgentStopped    Phase = "agent_stopped"
	PhaseFoundationError Phase = "foundation_error"
)

type ErrorMetadata struct {
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurred_at"`
}

// PluginState is a non-sensitive operation checkpoint. InputHash identifies
// normalized plugin input without retaining raw metadata or user-data.
type PluginState struct {
	Status         string `json:"status"`
	InputHash      string `json:"input_hash,omitempty"`
	RebootRequired bool   `json:"reboot_required"`
	Reused         bool   `json:"reused,omitempty"`
}

// Execution records irreversible intent, not a claim of exactly-once effects.
// A started/interrupted/failed operation is never automatically replayed.
type Execution struct {
	ApprovalHash string    `json:"approval_hash"`
	BootID       string    `json:"boot_id"`
	Status       string    `json:"status"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at,omitempty"`
	HasExitCode  bool      `json:"has_exit_code"`
	ExitCode     uint32    `json:"exit_code,omitempty"`
	OutputBytes  uint64    `json:"output_bytes,omitempty"`
	FailureCode  string    `json:"failure_code,omitempty"`
}

// State deliberately excludes credentials and raw metadata/user-data.
type State struct {
	SchemaVersion     int                    `json:"schema_version"`
	Generation        uint64                 `json:"generation"`
	InstanceID        string                 `json:"instance_id,omitempty"`
	Phase             Phase                  `json:"phase"`
	Attempts          map[string]int         `json:"attempts,omitempty"`
	RebootCount       int                    `json:"reboot_count"`
	Checkpoint        string                 `json:"checkpoint,omitempty"`
	LastError         *ErrorMetadata         `json:"last_error,omitempty"`
	UpdatedAt         time.Time              `json:"updated_at"`
	Provisioning      string                 `json:"provisioning,omitempty"`
	ConfigurationHash string                 `json:"configuration_hash,omitempty"`
	Plugins           map[string]PluginState `json:"plugins,omitempty"`
	Executions        map[string]Execution   `json:"executions,omitempty"`
}

func Initial(now time.Time) State {
	return State{SchemaVersion: CurrentSchemaVersion, Phase: PhaseNew, Attempts: make(map[string]int), UpdatedAt: now.UTC()}
}

func (s State) Validate() error {
	if s.SchemaVersion != CurrentSchemaVersion {
		return fmt.Errorf("%w: got %d, want %d", ErrUnsupportedSchema, s.SchemaVersion, CurrentSchemaVersion)
	}
	switch s.Phase {
	case PhaseNew, PhaseAgentRunning, PhaseAgentStopped, PhaseFoundationError:
	default:
		return fmt.Errorf("invalid lifecycle phase %q", s.Phase)
	}
	if s.RebootCount < 0 {
		return errors.New("reboot count must not be negative")
	}
	for name, attempts := range s.Attempts {
		if name == "" || attempts < 0 {
			return fmt.Errorf("invalid attempt counter %q=%d", name, attempts)
		}
	}
	for name, checkpoint := range s.Plugins {
		if name == "" {
			return errors.New("plugin checkpoint requires a name")
		}
		switch checkpoint.Status {
		case "disabled", "pending", "running", "completed", "reboot_required", "failed", "skipped":
		default:
			return fmt.Errorf("invalid plugin checkpoint status %q", checkpoint.Status)
		}
	}
	if len(s.Executions) > MaxExecutions {
		return errors.New("execution journal is full")
	}
	for key, execution := range s.Executions {
		if !ValidHash(key) || !ValidHash(execution.ApprovalHash) || !ValidHash(execution.BootID) || execution.StartedAt.IsZero() {
			return errors.New("invalid execution identity")
		}
		switch execution.Status {
		case "started":
			if execution.HasExitCode || execution.ExitCode != 0 || execution.OutputBytes != 0 || execution.FailureCode != "" || !execution.FinishedAt.IsZero() {
				return errors.New("started execution has a terminal result")
			}
		case "succeeded", "failed", "interrupted", "reboot_required":
			if execution.FinishedAt.IsZero() {
				return errors.New("terminal execution has no finish time")
			}
		default:
			return errors.New("invalid execution status")
		}
		if !execution.HasExitCode && execution.ExitCode != 0 {
			return errors.New("execution has an exit code without exit evidence")
		}
		if execution.OutputBytes > (1<<20)+1 {
			return errors.New("invalid execution output accounting")
		}
		if (execution.Status == "succeeded" || execution.Status == "reboot_required") && execution.FailureCode != "" {
			return errors.New("successful execution has a failure code")
		}
		if (execution.Status == "failed" || execution.Status == "interrupted") && execution.FailureCode == "" {
			return errors.New("failed execution has no failure code")
		}
		if execution.Status == "succeeded" && (!execution.HasExitCode || execution.ExitCode != 0) {
			return errors.New("successful execution has an invalid exit code")
		}
		if execution.Status == "reboot_required" && (!execution.HasExitCode || execution.ExitCode != 3010) {
			return errors.New("reboot execution has an invalid exit code")
		}
		switch execution.FailureCode {
		case "", "exit_failure", "output_limit", "timeout", "cancelled", "outcome_unknown", "plugin_failed", "protocol_error":
		default:
			return errors.New("invalid execution failure code")
		}
	}
	return nil
}

func ValidHash(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

type Store interface {
	Load(ctx context.Context) (State, error)
	Save(ctx context.Context, value State) error
}
