//go:build !windows

package hostname

import (
	"context"
	"errors"
	"testing"
)

func TestAdapterReportsUnsupported(t *testing.T) {
	t.Parallel()

	adapter := New()
	if _, _, err := adapter.Snapshot(context.Background()); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Snapshot() error = %v, want ErrUnsupported", err)
	}
	if err := adapter.Set(context.Background(), "WINNODE"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Set() error = %v, want ErrUnsupported", err)
	}
}

func TestAdapterHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	adapter := New()
	if _, _, err := adapter.Snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Snapshot() error = %v, want context.Canceled", err)
	}
	if err := adapter.Set(ctx, "WINNODE"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Set() error = %v, want context.Canceled", err)
	}
}
