//go:build windows

package state

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestRegistryStoreRoundTripAndRecovery(t *testing.T) {
	path := fmt.Sprintf(`SOFTWARE\WininitTests\%d`, time.Now().UnixNano())
	store := newRegistryStore(registry.CURRENT_USER, path)
	t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, path) })

	ctx := context.Background()
	if _, err := store.Load(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("initial Load() error = %v", err)
	}
	first := Initial(time.Now())
	first.Phase = PhaseAgentRunning
	if err := store.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Phase = PhaseAgentStopped
	if err := store.Save(ctx, second); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx)
	if err != nil || got.Phase != PhaseAgentStopped || got.Generation != 2 {
		t.Fatalf("Load() = %#v, %v", got, err)
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	active, _, err := key.GetStringValue(activeValue)
	if err != nil {
		key.Close()
		t.Fatal(err)
	}
	if err := key.SetBinaryValue(active, []byte("interrupted")); err != nil {
		key.Close()
		t.Fatal(err)
	}
	key.Close()

	recovered, err := store.Load(ctx)
	if err != nil || recovered.Phase != PhaseAgentRunning || recovered.Generation != 1 {
		t.Fatalf("recovered Load() = %#v, %v", recovered, err)
	}
}
