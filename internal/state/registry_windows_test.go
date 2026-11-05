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

func TestRegistryFutureSchemaDoesNotFallBackAndReplay(t *testing.T) {
	path := fmt.Sprintf(`SOFTWARE\WininitTests\%d`, time.Now().UnixNano())
	store := newRegistryStore(registry.CURRENT_USER, path)
	t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, path) })
	value := Initial(time.Now())
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	value.SchemaVersion++
	if err := key.SetBinaryValue(slotBValue, rawCheckpoint(t, value)); err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue(activeValue, slotBValue); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(context.Background()); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("Load error=%v", err)
	}
}

func TestRegistryRecoveryAndSaveRejectFutureSlot(t *testing.T) {
	for _, marker := range []string{"missing", "invalid", "wrong_type", "older_valid"} {
		t.Run(marker, func(t *testing.T) {
			path := fmt.Sprintf(`SOFTWARE\WininitTests\%d`, time.Now().UnixNano())
			store := newRegistryStore(registry.CURRENT_USER, path)
			t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, path) })
			value := Initial(time.Now())
			if err := store.Save(context.Background(), value); err != nil {
				t.Fatal(err)
			}
			key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ALL_ACCESS)
			if err != nil {
				t.Fatal(err)
			}
			defer key.Close()
			future := value
			future.SchemaVersion++
			if err := key.SetBinaryValue(slotBValue, rawCheckpoint(t, future)); err != nil {
				t.Fatal(err)
			}
			switch marker {
			case "missing":
				err = key.DeleteValue(activeValue)
			case "invalid":
				err = key.SetStringValue(activeValue, "broken")
			case "wrong_type":
				err = key.SetDWordValue(activeValue, 123)
			case "older_valid":
				err = key.SetStringValue(activeValue, slotAValue)
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(context.Background()); !errors.Is(err, ErrUnsupportedSchema) {
				t.Fatalf("Load() error=%v", err)
			}
			if err := store.Save(context.Background(), value); !errors.Is(err, ErrUnsupportedSchema) {
				t.Fatalf("Save() error=%v", err)
			}
		})
	}
}
