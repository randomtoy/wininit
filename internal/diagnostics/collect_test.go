package diagnostics

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/randomtoy/wininit/internal/config"
)

func TestCollectAllowlistsLogsAndSanitizedConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Log.Directory = filepath.Join(dir, "logs")
	if err := os.MkdirAll(cfg.Log.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Log.Directory, "wininit.log"), []byte("safe log"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Log.Directory, "raw-user-data"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "diagnostics.zip")
	if err := Collect(context.Background(), cfg, map[string]string{"state": "ok"}, output); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, file := range zr.File {
		names[file.Name] = true
	}
	if !names["manifest.json"] || !names["status.json"] || !names["config/wininit.sanitized.yaml"] || !names["logs/wininit.log"] {
		t.Fatalf("archive entries = %#v", names)
	}
	if names["logs/raw-user-data"] {
		t.Fatal("non-log file entered diagnostics archive")
	}
}
