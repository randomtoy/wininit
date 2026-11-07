package pluginprocess

import (
	"context"
	"errors"
	"github.com/randomtoy/wininit/internal/plugin"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hostnameSettings(t *testing.T, mode string) ProvisionerSettings {
	t.Helper()
	return ProvisionerSettings{AllowPrivileged: true, Process: processSettings(t, mode)}
}
func TestExternalProvisionerOutcomesAndFailures(t *testing.T) {
	for _, tc := range []struct {
		mode    string
		want    plugin.Result
		failure bool
	}{
		{"prov-ok", plugin.Result{}, false}, {"prov-changed", plugin.Result{Changed: true}, false}, {"prov-reboot", plugin.Result{RebootRequired: true}, false}, {"prov-changed-reboot", plugin.Result{Changed: true, RebootRequired: true}, false}, {"prov-skipped", plugin.Result{Skipped: true}, false},
		{"prov-identity", plugin.Result{}, true}, {"prov-kind", plugin.Result{}, true}, {"prov-wire-version", plugin.Result{}, true}, {"prov-version", plugin.Result{}, true}, {"prov-once", plugin.Result{}, true}, {"prov-invalid", plugin.Result{}, true}, {"prov-unknown", plugin.Result{}, true}, {"prov-error", plugin.Result{}, true}, {"prov-earlydeadline", plugin.Result{}, true}, {"prov-crash", plugin.Result{}, true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			settings := hostnameSettings(t, tc.mode)
			value, err := NewHostname(settings).Apply(context.Background(), plugin.Metadata{InstanceID: "fixture", LocalHostname: "node", Datasource: "nocloud"})
			if tc.failure {
				if err == nil || strings.Contains(err.Error(), "fixture-private-secret") || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("unsafe failure: %#v %v", value, err)
				}
			} else if err != nil || value != tc.want {
				t.Fatalf("result=%#v error=%v", value, err)
			}
		})
	}
}
func TestExternalProvisionerRequiresApprovalAndPin(t *testing.T) {
	settings := hostnameSettings(t, "prov-ok")
	metadata := plugin.Metadata{InstanceID: "fixture", Datasource: "nocloud"}
	settings.AllowPrivileged = false
	if _, err := NewHostname(settings).Apply(context.Background(), metadata); err == nil {
		t.Fatal("ran without privileged approval")
	}
	settings.AllowPrivileged = true
	settings.Process.SHA256 = strings.Repeat("0", 64)
	if _, err := NewHostname(settings).Apply(context.Background(), metadata); err == nil {
		t.Fatal("ran unapproved image")
	}
	settings.Process.Executable = filepath.Join(t.TempDir(), "absent.exe")
	if _, err := NewHostname(settings).Apply(context.Background(), plugin.Metadata{}); err == nil || !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("invalid metadata did not fail before image access: %v", err)
	}
}
func TestExternalProvisionerCancellationAndTimeout(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		cancel     bool
	}{{"apply", "prov-hang", true}, {"describe", "prov-describe-hang", true}, {"handshake", "prov-nohandshake", true}, {"private-timeout", "prov-hang", false}} {
		t.Run(tc.name, func(t *testing.T) {
			settings := hostnameSettings(t, tc.mode)
			settings.Process.TimeoutSeconds = 3
			ctx := context.Background()
			if tc.cancel {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 250*time.Millisecond)
				defer stop()
			}
			start := time.Now()
			_, err := NewHostname(settings).Apply(ctx, plugin.Metadata{InstanceID: "fixture", Datasource: "nocloud"})
			if tc.cancel {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("root cancellation=%v", err)
				}
			} else if !errors.Is(err, errProvisionerTimeout) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("private timeout hidden as root cancellation: %v", err)
			}
			if time.Since(start) > 7*time.Second {
				t.Fatal("process cleanup missed cancellation")
			}
		})
	}
}
func TestExternalProvisionerEnvironment(t *testing.T) {
	t.Setenv("WININIT_TEST_SECRET", "fixture-private-secret")
	_, err := NewHostname(hostnameSettings(t, "prov-environment")).Apply(context.Background(), plugin.Metadata{InstanceID: "fixture", Datasource: "nocloud"})
	if err != nil {
		t.Fatal(err)
	}
}
func TestExternalNoCloudReadsRealSeed(t *testing.T) {
	settings := processSettings(t, "nocloud")
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "meta-data"), []byte("instance-id: external-nocloud\nlocal-hostname: NodeName\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// The datasource must ignore even malformed executable user-data.
	if err := os.WriteFile(filepath.Join(directory, "user-data"), []byte("fixture-private-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	prepareTestPath(t, directory)
	value, err := NewNoCloud(settings, directory, false).Discover(context.Background())
	if err != nil || value.InstanceID != "external-nocloud" || value.LocalHostname != "NodeName" {
		t.Fatalf("metadata=%#v err=%v", value, err)
	}
	if err := os.Remove(filepath.Join(directory, "meta-data")); err != nil {
		t.Fatal(err)
	}
	if _, err := NewNoCloud(settings, directory, false).Discover(context.Background()); !errors.Is(err, plugin.ErrUnavailable) {
		t.Fatalf("missing seed=%v", err)
	}
}
