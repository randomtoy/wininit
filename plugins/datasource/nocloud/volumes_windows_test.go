//go:build windows

package nocloud

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFindVolumesReadOnly(t *testing.T) {
	roots, err := FindVolumes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			t.Fatalf("discovery returned non-absolute root: %q", root)
		}
	}
}
