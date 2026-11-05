package state

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCheckpointRoundTrip(t *testing.T) {
	t.Parallel()
	want := Initial(time.Unix(100, 0))
	want.Generation = 7
	want.Attempts["datasource"] = 2
	data, err := encodeCheckpoint(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeCheckpoint(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.Generation != want.Generation || got.Attempts["datasource"] != 2 || !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("round trip got %#v, want %#v", got, want)
	}
}

func TestCheckpointDetectsCorruption(t *testing.T) {
	t.Parallel()
	data, err := encodeCheckpoint(Initial(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 1
	if _, err := decodeCheckpoint(data); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("decodeCheckpoint() error = %v, want ErrCorrupt", err)
	}
}

func TestCheckpointRejectsFutureSchema(t *testing.T) {
	t.Parallel()
	value := Initial(time.Now())
	value.SchemaVersion++
	if _, err := encodeCheckpoint(value); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("encodeCheckpoint() error = %v, want ErrUnsupportedSchema", err)
	}
}

func TestMemoryStoreCopiesState(t *testing.T) {
	t.Parallel()
	store := NewMemoryStore()
	value := Initial(time.Now())
	value.Attempts["x"] = 1
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	value.Attempts["x"] = 99
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts["x"] != 1 {
		t.Fatalf("stored map was aliased: %#v", got.Attempts)
	}
	if got.Generation != 1 {
		t.Fatalf("first stored generation = %d, want 1", got.Generation)
	}
	if err := store.Save(context.Background(), got); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load(context.Background())
	if err != nil || got.Generation != 2 {
		t.Fatalf("second stored generation = %d, %v; want 2", got.Generation, err)
	}
}

func TestMemoryStoreHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewMemoryStore().Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Load() error = %v", err)
	}
}
