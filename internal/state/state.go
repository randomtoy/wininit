// Package state defines persistent lifecycle state and storage contracts.
package state

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const CurrentSchemaVersion = 1

var (
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

// State deliberately excludes credentials and raw metadata/user-data.
type State struct {
	SchemaVersion int            `json:"schema_version"`
	Generation    uint64         `json:"generation"`
	InstanceID    string         `json:"instance_id,omitempty"`
	Phase         Phase          `json:"phase"`
	Attempts      map[string]int `json:"attempts,omitempty"`
	RebootCount   int            `json:"reboot_count"`
	Checkpoint    string         `json:"checkpoint,omitempty"`
	LastError     *ErrorMetadata `json:"last_error,omitempty"`
	UpdatedAt     time.Time      `json:"updated_at"`
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
	return nil
}

type Store interface {
	Load(ctx context.Context) (State, error)
	Save(ctx context.Context, value State) error
}
