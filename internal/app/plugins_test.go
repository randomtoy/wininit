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
	if len(descriptors) != 2 {
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
			if report.Provisioning != tc.want || report.Plugins[1].Status != tc.want {
				t.Fatalf("report = %#v", report)
			}
		})
	}
}
