package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValid(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, `
version: 1
log:
  level: info
  directory: 'C:\ProgramData\wininit\logs'
state:
  registry_path: 'SOFTWARE\Wininit'
plugins:
  directory: 'C:\Program Files\wininit\plugins'
reboot:
  policy: never
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Version != CurrentVersion || cfg.Log.Level != "info" {
		t.Fatalf("Load() = %#v", cfg)
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, "version: 1\nunknown: true\n")
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "field unknown not found") {
		t.Fatalf("Load() error = %v, want unknown-field error", err)
	}
}

func TestLoadAppliesDefaultsButRequiresVersion(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, "version: 1\n")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Log.Level != "info" || cfg.Plugins.Directory == "" || cfg.Reboot.Policy != "never" {
		t.Fatalf("Load() defaults = %#v", cfg)
	}
	missing := writeConfig(t, "log:\n  level: info\n")
	if _, err := Load(missing); err == nil || !strings.Contains(err.Error(), "explicitly specified") {
		t.Fatalf("Load() missing-version error = %v", err)
	}
}

func TestLoadRejectsMultipleDocuments(t *testing.T) {
	t.Parallel()
	path := writeConfig(t, "version: 1\n---\nversion: 1\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "multiple YAML documents") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestValidateReturnsMultipleErrors(t *testing.T) {
	t.Parallel()
	err := (Config{Version: 99}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil")
	}
	for _, want := range []string{"version", "log.level", "log.directory", "state.registry_path", "plugins.directory", "reboot.policy"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() error %q does not contain %q", err, want)
		}
	}
}

func TestDefaultIsValid(t *testing.T) {
	t.Parallel()
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() error = %v", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wininit.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
