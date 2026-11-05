package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func rawCheckpoint(t *testing.T, value State) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	encoded, err := json.Marshal(envelope{Format: 1, Payload: payload, Checksum: hex.EncodeToString(sum[:])})
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestSchemaOneMigrationDoesNotInventProvisioningSuccess(t *testing.T) {
	t.Parallel()
	old := Initial(time.Now())
	old.SchemaVersion = 1
	old.InstanceID = "existing-instance"
	got, err := decodeCheckpoint(rawCheckpoint(t, old))
	if err != nil || got.SchemaVersion != 2 || got.InstanceID != old.InstanceID || got.Provisioning != "disabled" {
		t.Fatalf("migrated=%#v err=%v", got, err)
	}
}

func TestDecodeRejectsFutureSchema(t *testing.T) {
	t.Parallel()
	value := Initial(time.Now())
	value.SchemaVersion++
	if _, err := decodeCheckpoint(rawCheckpoint(t, value)); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("error = %v", err)
	}
}

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
