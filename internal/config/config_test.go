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

func TestPluginsAreOptInForExistingConfiguration(t *testing.T) {
	t.Parallel()
	cfg, err := Load(writeConfig(t, "version: 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Datasources.NoCloud.Enabled || cfg.Plugins.Datasources.ConfigDrive.Enabled || cfg.Plugins.Provisioners.Hostname.Enabled {
		t.Fatal("old configuration silently enabled plugins")
	}
}

func TestConfigDriveConfigurationAndBackwardCompatibility(t *testing.T) {
	t.Parallel()
	cfg, err := Load(writeConfig(t, "version: 1\nplugins:\n  datasources:\n    nocloud:\n      enabled: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Plugins.Datasources.NoCloud.Enabled || cfg.Plugins.Datasources.ConfigDrive.Enabled {
		t.Fatal("old NoCloud configuration activated ConfigDrive")
	}
	cfg, err = Load(writeConfig(t, "version: 1\nplugins:\n  datasources:\n    configdrive:\n      enabled: true\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Plugins.Datasources.ConfigDrive.Enabled || cfg.Plugins.Datasources.NoCloud.Enabled || cfg.Plugins.Provisioners.Hostname.Enabled {
		t.Fatal("ConfigDrive is not independently configurable")
	}
	if !cfg.Plugins.Datasources.ConfigDrive.DiscoverVolumes {
		t.Fatal("missing safe volume discovery default")
	}
	cfg.Plugins.Datasources.ConfigDrive.DiscoverVolumes = false
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled ConfigDrive accepted no source")
	}
	cfg.Plugins.Datasources.ConfigDrive.SeedDirectory = t.TempDir()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, directory := range []string{"relative", `\\server\share`, "https://metadata.invalid/", "//server/share"} {
		cfg.Plugins.Datasources.ConfigDrive.SeedDirectory = directory
		if err := cfg.Validate(); err == nil {
			t.Errorf("accepted non-local seed_directory %q", directory)
		}
	}
	cfg.Plugins.Datasources.ConfigDrive.Enabled = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled ConfigDrive requires a usable source: %v", err)
	}
}

func TestDisabledPluginOptionsDoNotRequireUsableSource(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Plugins.Datasources.NoCloud.DiscoverVolumes = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled plugin requires a source: %v", err)
	}
	cfg.Plugins.Datasources.NoCloud.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("enabled plugin accepted no source")
	}
	cfg.Plugins.Datasources.NoCloud.SeedDirectory = t.TempDir()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPluginConfigurationRejectsMisspelledNames(t *testing.T) {
	t.Parallel()
	_, err := Load(writeConfig(t, "version: 1\nplugins:\n  provisioners:\n    hostnmae:\n      enabled: true\n"))
	if err == nil {
		t.Fatal("unknown plugin name accepted")
	}
}

func TestRegistryPathRequiresDedicatedSubkey(t *testing.T) {
	t.Parallel()
	for _, path := range []string{`SOFTWARE\`, `SOFTWARE\Wininit\`, `SOFTWARE\\Wininit`, "SOFTWARE\\Wininit\x00bad", "SOFTWARE\\Wininit/other", " SOFTWARE\\Wininit"} {
		cfg := Default()
		cfg.State.RegistryPath = path
		if err := cfg.Validate(); err == nil {
			t.Errorf("accepted unsafe Registry path %q", path)
		}
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
