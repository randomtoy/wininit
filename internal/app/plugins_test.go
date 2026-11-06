package app

import (
	"testing"

	"github.com/randomtoy/wininit/internal/config"
	"github.com/randomtoy/wininit/internal/state"
)

func TestDefaultPluginStatusAndConfigurationFingerprint(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	if configuredProvisioning(cfg) != "disabled" {
		t.Fatal("default configuration permits provisioning")
	}
	descriptors := configuredPlugins(cfg).Descriptors()
	if len(descriptors) != 3 {
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
