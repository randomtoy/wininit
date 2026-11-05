//go:build windows

package state

import (
	"context"
	"testing"
)

func TestMachineLockExcludesSecondLifecycleAndReleases(t *testing.T) {
	release, err := AcquireMachineLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second, err := AcquireMachineLock(context.Background()); err == nil {
		second()
		release()
		t.Fatal("concurrent lifecycle acquired machine lock")
	}
	release()
	release, err = AcquireMachineLock(context.Background())
	if err != nil {
		t.Fatalf("released lock cannot be acquired: %v", err)
	}
	release()
}
