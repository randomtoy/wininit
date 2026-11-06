package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/pluginprocess"
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
	if cfg.Plugins.Datasources.NoCloud.Enabled || cfg.Plugins.Datasources.ConfigDrive.Enabled || cfg.Plugins.Provisioners.Hostname.Enabled || cfg.Plugins.Provisioners.LocalUsers.Enabled || cfg.Plugins.Provisioners.PasswordPolicy.Enabled || cfg.Plugins.Provisioners.AuthorizedKeys.Enabled {
		t.Fatal("old configuration silently enabled plugins")
	}
}

func TestIdentityConfigurationIndependentGatesAndRedaction(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Plugins.Provisioners.LocalUsers.Users = []identity.User{{Name: "worker", PasswordFile: filepath.Join(t.TempDir(), "initial-password")}}
	cfg.Plugins.Provisioners.AuthorizedKeys.Users = []identity.KeyUser{{Name: "worker"}}
	for _, pluginName := range []string{"local_users", "password_policy", "authorized_keys"} {
		current := cfg
		current.Plugins.Provisioners.LocalUsers.Enabled = pluginName == "local_users"
		current.Plugins.Provisioners.PasswordPolicy.Enabled = pluginName == "password_policy"
		current.Plugins.Provisioners.AuthorizedKeys.Enabled = pluginName == "authorized_keys"
		if err := current.Validate(); err != nil {
			t.Fatalf("%s: %v", pluginName, err)
		}
	}
	safe := cfg.Sanitized()
	if len(safe.Plugins.Provisioners.LocalUsers.Users) != 0 || len(safe.Plugins.Provisioners.AuthorizedKeys.Users) != 0 || len(cfg.Plugins.Provisioners.LocalUsers.Users) != 1 {
		t.Fatal("sanitization leaked identity settings or changed live configuration")
	}
	cfg.Plugins.Provisioners.LocalUsers.Enabled = true
	cfg.Plugins.Provisioners.LocalUsers.Users[0].PasswordFile = filepath.Join(cfg.Log.Directory, "password.log")
	if err := cfg.Validate(); err == nil {
		t.Fatal("password file inside logs accepted")
	}
	cfg.Plugins.Provisioners.LocalUsers.Enabled = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled identity options required valid input: %v", err)
	}
}

func TestExternalConfigDriveRequiresExactPinOnlyWhenEnabled(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.Plugins.Datasources.ConfigDrive.Execution = "external"
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Plugins.Datasources.ConfigDrive.Enabled = true
	if err := cfg.Validate(); err == nil {
		t.Fatal("external mode accepted an unpinned executable")
	}
	cfg.Plugins.Datasources.ConfigDrive.Process = pluginprocess.Settings{Executable: filepath.Join(t.TempDir(), "plugin.exe"), SHA256: strings.Repeat("a", 64), TimeoutSeconds: 15}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.Plugins.Datasources.ConfigDrive.Execution = "automatic"
	if err := cfg.Validate(); err == nil {
		t.Fatal("unknown execution mode accepted")
	}
}

func TestIdentitySchemaRejectsPlaintextPasswordsAndMalformedInput(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		"local_users:\n      enabled: true\n      users: []",
		"local_users:\n      enabled: true\n      users:\n        - name: worker\n          password: plaintext-not-supported",
		"password_policy:\n      enabled: true\n      minimum_length: 1",
		"authorized_keys:\n      enabled: true\n      users:\n        - name: worker\n          public_keys: ['PRIVATE KEY']",
	} {
		if _, err := Load(writeConfig(t, "version: 1\nplugins:\n  provisioners:\n    "+body+"\n")); err == nil {
			t.Fatal("unsafe identity configuration accepted")
		}
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

func TestPowerShellRequiresIndependentAuthorizationAndRedactsExecutionID(t *testing.T) {
	cfg := Default()
	if cfg.Plugins.Provisioners.PowerShellUserData.Enabled || cfg.Plugins.Provisioners.PowerShellUserData.AllowPrivileged {
		t.Fatal("privileged execution enabled by default")
	}
	cfg.Plugins.Provisioners.PowerShellUserData.ExecutionID = "private-operation-label"
	if cfg.Sanitized().Plugins.Provisioners.PowerShellUserData.ExecutionID != "" {
		t.Fatal("diagnostics disclosed execution label")
	}
	cfg.Plugins.Provisioners.PowerShellUserData.Enabled = true
	if cfg.Validate() == nil {
		t.Fatal("privileged execution accepted without explicit authorization and pins")
	}
}
