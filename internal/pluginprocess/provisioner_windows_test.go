//go:build windows

package pluginprocess

import (
	"context"
	platformhostname "github.com/randomtoy/wininit/internal/platform/hostname"
	"github.com/randomtoy/wininit/internal/plugin"
	"strings"
	"testing"
)

func TestWindowsExternalHostnameReconcilesWithoutMutation(t *testing.T) {
	adapter := platformhostname.New()
	active, configured, err := adapter.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	settings := hostnameSettings(t, "hostname")
	for i := 0; i < 2; i++ {
		value, err := NewHostname(settings).Apply(context.Background(), plugin.Metadata{InstanceID: "native-hostname", LocalHostname: configured, Datasource: "nocloud"})
		if err != nil || value.Changed || value.Skipped || value.RebootRequired != !strings.EqualFold(active, configured) {
			t.Fatalf("no-mutation reconciliation=%#v %v", value, err)
		}
	}
	afterActive, afterConfigured, err := adapter.Snapshot(context.Background())
	if err != nil || afterActive != active || afterConfigured != configured {
		t.Fatal("native test changed the computer name", err)
	}
}
func TestWindowsDeclarativeProvisionerRole(t *testing.T) {
	_, err := NewHostname(hostnameSettings(t, "prov-restrictions")).Apply(context.Background(), plugin.Metadata{InstanceID: "native-role", Datasource: "nocloud"})
	if err != nil {
		t.Fatal(err)
	}
}
