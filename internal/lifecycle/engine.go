// Package lifecycle owns truthful state transitions for the agent process.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/randomtoy/wininit/internal/state"
)

type Engine struct {
	store  state.Store
	logger *slog.Logger
	now    func() time.Time
}

func New(store state.Store, logger *slog.Logger) *Engine {
	return &Engine{store: store, logger: logger, now: time.Now}
}

// Run records that the agent foundation is active, then waits for cancellation.
// It intentionally performs and claims no provisioning work.
func (e *Engine) Run(ctx context.Context) error {
	current, err := e.store.Load(ctx)
	if errors.Is(err, state.ErrNotFound) {
		current = state.Initial(e.now())
	} else if err != nil {
		return fmt.Errorf("load lifecycle state: %w", err)
	}
	if err := Transition(current.Phase, state.PhaseAgentRunning); err != nil {
		return err
	}
	current.Phase = state.PhaseAgentRunning
	current.LastError = nil
	current.UpdatedAt = e.now().UTC()
	if err := e.store.Save(ctx, current); err != nil {
		return fmt.Errorf("save running lifecycle state: %w", err)
	}
	e.logger.InfoContext(ctx, "lifecycle foundation started", "provisioning", "not_implemented")

	<-ctx.Done()
	stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	current.Phase = state.PhaseAgentStopped
	current.UpdatedAt = e.now().UTC()
	if err := e.store.Save(stopCtx, current); err != nil {
		return fmt.Errorf("save stopped lifecycle state: %w", err)
	}
	e.logger.Info("lifecycle foundation stopped")
	return nil
}

func Transition(from, to state.Phase) error {
	allowed := false
	switch to {
	case state.PhaseAgentRunning:
		allowed = from == state.PhaseNew || from == state.PhaseAgentStopped || from == state.PhaseFoundationError || from == state.PhaseAgentRunning
	case state.PhaseAgentStopped, state.PhaseFoundationError:
		allowed = from == state.PhaseAgentRunning
	}
	if !allowed {
		return fmt.Errorf("invalid lifecycle transition %q -> %q", from, to)
	}
	return nil
}
