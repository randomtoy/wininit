//go:build windows

package configdrive

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
			t.Fatalf("non-absolute ConfigDrive root: %q", root)
		}
	}
}
