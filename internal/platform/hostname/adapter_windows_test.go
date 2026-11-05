//go:build windows

package hostname

import (
	"context"
	"testing"
)

func TestSnapshotReadsWindowsHostnameWithoutMutation(t *testing.T) {
	t.Parallel()

	active, configured, err := New().Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if active == "" {
		t.Error("Snapshot() active hostname is empty")
	}
	if configured == "" {
		t.Error("Snapshot() configured hostname is empty")
	}
}
