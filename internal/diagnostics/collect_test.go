package diagnostics

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
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

func TestConfigDriveMetadataAndUserDataAreNeverArchived(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	cfg := config.Default()
	cfg.Log.Directory = filepath.Join(directory, "logs")
	seed := filepath.Join(directory, "seed")
	cfg.Plugins.Datasources.ConfigDrive.Enabled = true
	cfg.Plugins.Datasources.ConfigDrive.SeedDirectory = seed
	metadataDirectory := filepath.Join(seed, "openstack", "latest")
	if err := os.MkdirAll(metadataDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := []byte("configdrive-fixture-sensitive-value")
	for _, name := range []string{"meta_data.json", "user_data", "password", "vendor_data.json"} {
		if err := os.WriteFile(filepath.Join(metadataDirectory, name), secret, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	output := filepath.Join(directory, "diagnostics.zip")
	if err := Collect(context.Background(), cfg, map[string]string{"provisioning": "metadata_available"}, output); err != nil {
		t.Fatal(err)
	}
	reader, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	for _, entry := range reader.File {
		file, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, secret) {
			t.Fatalf("ConfigDrive contents leaked through %s", entry.Name)
		}
	}
}
