// Package lifecycle owns truthful state transitions for the agent process.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/state"
)

type Engine struct {
	store             state.Store
	logger            *slog.Logger
	now               func() time.Time
	catalog           plugin.Catalog
	configurationHash string
	bootID            func(context.Context) (string, error)
}

func New(store state.Store, logger *slog.Logger) *Engine {
	return &Engine{store: store, logger: logger, now: time.Now, bootID: func(ctx context.Context) (string, error) {
		if boots, ok := store.(interface {
			BootID(context.Context) (string, error)
		}); ok {
			return boots.BootID(ctx)
		}
		return "", state.ErrUnsupported
	}}
}

func NewWithPlugins(store state.Store, logger *slog.Logger, catalog plugin.Catalog, configurationHash string) *Engine {
	engine := New(store, logger)
	engine.catalog, engine.configurationHash = catalog, configurationHash
	return engine
}

// Run reconciles enabled plugins once, then waits for cancellation. A restart
// retries failed work and checks pending changes against actual platform state.
func (e *Engine) Run(ctx context.Context) error {
	e.logger.InfoContext(ctx, "loading lifecycle checkpoint")
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
	e.logger.InfoContext(ctx, "saving running lifecycle checkpoint")
	if err := e.store.Save(ctx, current); err != nil {
		return fmt.Errorf("save running lifecycle state: %w", err)
	}
	e.logger.InfoContext(ctx, "running lifecycle checkpoint saved")
	if err := e.reconcile(ctx, &current); err != nil {
		if !rootCancellationOnly(ctx, err) {
			return fmt.Errorf("reconcile plugins: %w", err)
		}
		current.Provisioning = "pending"
		current.LastError = nil
		for name, checkpoint := range current.Plugins {
			if checkpoint.Status == "running" || checkpoint.Status == "failed" {
				checkpoint.Status = "pending"
				current.Plugins[name] = checkpoint
			}
		}
	}
	e.logger.InfoContext(ctx, "lifecycle started", "provisioning", current.Provisioning)

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
