package lifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/randomtoy/wininit/internal/state"
)

type failingStore struct{ err error }

func (f failingStore) Load(context.Context) (state.State, error) { return state.State{}, f.err }
func (f failingStore) Save(context.Context, state.State) error   { return f.err }

func TestTransitions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		from, to state.Phase
		valid    bool
	}{
		{state.PhaseNew, state.PhaseAgentRunning, true},
		{state.PhaseAgentStopped, state.PhaseAgentRunning, true},
		{state.PhaseAgentRunning, state.PhaseAgentStopped, true},
		{state.PhaseNew, state.PhaseAgentStopped, false},
		{state.PhaseAgentStopped, state.PhaseFoundationError, false},
	} {
		err := Transition(tc.from, tc.to)
		if (err == nil) != tc.valid {
			t.Errorf("Transition(%q, %q) error = %v, valid=%v", tc.from, tc.to, err, tc.valid)
		}
	}
}

func TestEngineRunStopsCleanly(t *testing.T) {
	t.Parallel()
	store := state.NewMemoryStore()
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	engine := New(store, logger)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		current, err := store.Load(context.Background())
		if err == nil && current.Phase == state.PhaseAgentRunning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("engine did not enter running phase")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	current, err := store.Load(context.Background())
	if err != nil || current.Phase != state.PhaseAgentStopped {
		t.Fatalf("final state = %#v, %v", current, err)
	}
}

func TestEnginePropagatesLoadError(t *testing.T) {
	t.Parallel()
	want := errors.New("registry access denied")
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	err := New(failingStore{err: want}, logger).Run(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want wrapped %v", err, want)
	}
}
