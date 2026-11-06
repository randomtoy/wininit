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
	"github.com/randomtoy/wininit/internal/identity"
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

func TestIdentitySettingsSecretsAndMisnamedLogsNeverArchived(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	cfg := config.Default()
	cfg.Log.Directory = filepath.Join(directory, "logs")
	if err := os.MkdirAll(cfg.Log.Directory, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := []byte("identity-fixture-sensitive-password")
	password := filepath.Join(directory, "password")
	if err := os.WriteFile(password, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	// Even a hard link under the agent's exact log name must be excluded.
	if err := os.Link(password, filepath.Join(cfg.Log.Directory, "wininit.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Log.Directory, "password.log"), secret, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.Plugins.Provisioners.LocalUsers.Users = []identity.User{{Name: "private-account-name", PasswordFile: password}}
	cfg.Plugins.Provisioners.AuthorizedKeys.Users = []identity.KeyUser{{Name: "private-account-name", PublicKeys: []string{"private-public-key-fixture"}}}
	output := filepath.Join(directory, "diagnostics.zip")
	if err := Collect(context.Background(), cfg, map[string]string{"provisioning": "pending"}, output); err != nil {
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
		for _, forbidden := range [][]byte{secret, []byte("private-account-name"), []byte("private-public-key-fixture"), []byte(password)} {
			if bytes.Contains(data, forbidden) {
				t.Fatalf("identity data leaked through %s", entry.Name)
			}
		}
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
