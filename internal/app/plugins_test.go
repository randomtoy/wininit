package app

import (
	"testing"

	"github.com/randomtoy/wininit/internal/config"
	"github.com/randomtoy/wininit/internal/pluginprocess"
	"github.com/randomtoy/wininit/internal/state"
)

func TestDefaultPluginStatusAndConfigurationFingerprint(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	if configuredProvisioning(cfg) != "disabled" {
		t.Fatal("default configuration permits provisioning")
	}
	descriptors := configuredPlugins(cfg).Descriptors()
	if len(descriptors) != 6 {
		t.Fatalf("descriptors=%#v", descriptors)
	}
	for _, descriptor := range descriptors {
		if descriptor.Enabled || descriptor.Status != "disabled" {
			t.Fatalf("unexpected default plugin status: %#v", descriptor)
		}
	}
	old := pluginConfigurationHash(cfg)
	cfg.Plugins.Provisioners.Hostname.Enabled = true
	if pluginConfigurationHash(cfg) == old {
		t.Fatal("plugin enablement did not invalidate old runtime status")
	}
}

func TestIdentityEnablementOrderAndExternalConstruction(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Plugins.Datasources.ConfigDrive.Execution = "external"
	cfg.Plugins.Datasources.ConfigDrive.Process.Executable = "/absent/plugin.exe"
	catalog := configuredPlugins(cfg)
	if _, ok := catalog.Datasources[1].Plugin.(*pluginprocess.ConfigDrive); !ok {
		t.Fatal("external ConfigDrive mode retained the built-in implementation")
	}
	want := []string{"password_policy", "local_users", "authorized_keys", "hostname"}
	for index, registration := range catalog.Provisioners {
		if registration.Plugin.Name() != want[index] || registration.Enabled {
			t.Fatalf("unsafe provisioner order/gate: %#v", registration)
		}
	}
	for _, name := range want[:3] {
		current := cfg
		current.Plugins.Provisioners.PasswordPolicy.Enabled = name == "password_policy"
		current.Plugins.Provisioners.LocalUsers.Enabled = name == "local_users"
		current.Plugins.Provisioners.AuthorizedKeys.Enabled = name == "authorized_keys"
		if configuredProvisioning(current) != "pending" {
			t.Fatalf("%s enablement ignored", name)
		}
		enabled := 0
		for _, descriptor := range configuredPlugins(current).Descriptors() {
			if descriptor.Enabled {
				enabled++
				if descriptor.Name != name {
					t.Fatal("independent enablement activated another plugin")
				}
			}
		}
		if enabled != 1 {
			t.Fatal("identity plugin has no independent gate")
		}
	}
}

func TestSavedStatusCannotOverrideDisablementOrNewConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		enabled         bool
		savedHash, want string
	}{
		{"disabled pending checkpoint", false, "current", "disabled"},
		{"changed configuration", true, "old", "pending"},
		{"matching configuration", true, "current", "reboot_required"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.Default()
			cfg.Plugins.Provisioners.Hostname.Enabled = tc.enabled
			report := Status{Provisioning: configuredProvisioning(cfg), Plugins: configuredPlugins(cfg).Descriptors()}
			current := state.State{ConfigurationHash: tc.savedHash, Provisioning: "reboot_required", Plugins: map[string]state.PluginState{"hostname": {Status: "reboot_required", RebootRequired: true}}}
			applySavedPluginStatus(&report, current, "current")
			if report.Provisioning != tc.want {
				t.Fatalf("report = %#v", report)
			}
			for _, descriptor := range report.Plugins {
				if descriptor.Name == "hostname" && descriptor.Status != tc.want {
					t.Fatalf("hostname descriptor = %#v", descriptor)
				}
			}
		})
	}
}

func TestConfigDriveIsIndependentlyEnabledAndReported(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	previousHash := pluginConfigurationHash(cfg)
	cfg.Plugins.Datasources.ConfigDrive.Enabled = true
	if configuredProvisioning(cfg) != "pending" || pluginConfigurationHash(cfg) == previousHash {
		t.Fatal("ConfigDrive enablement was not reflected in status or fingerprint")
	}
	for _, descriptor := range configuredPlugins(cfg).Descriptors() {
		want := descriptor.Name == "configdrive"
		if descriptor.Enabled != want {
			t.Fatalf("unexpected plugin enablement: %#v", descriptor)
		}
	}
}
