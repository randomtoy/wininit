package pluginprocess

import (
	"context"
	"errors"
	"github.com/randomtoy/wininit/internal/plugin"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func psSettings(t *testing.T, mode string) PowerShellSettings {
	t.Helper()
	process := processSettings(t, mode)
	return PowerShellSettings{Enabled: true, AllowPrivileged: true, ExecutionID: "first", ScriptSHA256: strings.Repeat("b", 64), MaxOutputBytes: 1024, Process: process}
}
func psAdapter(settings PowerShellSettings) *PowerShell {
	return NewPowerShell(settings, map[string]SourceSettings{"nocloud": {Directory: filepath.Join(filepath.Dir(settings.Process.Executable), "unused-seed")}})
}
func TestPowerShellWireBoundariesAndRedaction(t *testing.T) {
	for _, test := range []struct {
		mode, message string
		prepare       bool
	}{
		{"ps-version", "handshake", true}, {"ps-identity", "identity", true}, {"ps-leak", "preparation", true}, {"ps-invalid", "prepared script identity", true}, {"ps-conflict", "conflicting preparation", true},
		{"ps-error", "execution", false}, {"ps-failed", "exit code 17", false}, {"ps-outputlimit", "output limit", false}, {"ps-oversized", "output accounting", false}, {"ps-overflow-success", "output accounting", false}, {"ps-conflicting-success", "conflicting success", false},
	} {
		t.Run(test.mode, func(t *testing.T) {
			t.Parallel()
			p := psAdapter(psSettings(t, test.mode))
			plan, err := p.Prepare(context.Background(), plugin.Metadata{InstanceID: "fixture", Datasource: "nocloud"})
			if plan.Close != nil {
				defer plan.Close()
			}
			if !test.prepare && err == nil {
				_, err = plan.Execute(context.Background())
			}
			if err == nil || !strings.Contains(err.Error(), test.message) || strings.Contains(err.Error(), "fixture-private-secret") {
				t.Fatalf("wrong boundary or leaked output: %v", err)
			}
		})
	}
}
func TestPowerShellWireUnavailableSuccessRebootAndEnvironment(t *testing.T) {
	t.Setenv("WININIT_TEST_SECRET", "fixture-private-secret")
	for _, mode := range []string{"ps-unavailable", "ps-success", "ps-reboot", "ps-environment"} {
		t.Run(mode, func(t *testing.T) {
			p := psAdapter(psSettings(t, mode))
			plan, err := p.Prepare(context.Background(), plugin.Metadata{InstanceID: "fixture", Datasource: "nocloud"})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Close != nil {
				defer plan.Close()
			}
			if mode == "ps-unavailable" {
				if !plan.Skipped || plan.Execute != nil {
					t.Fatal("missing payload can execute")
				}
				return
			}
			result, err := plan.Execute(context.Background())
			if err != nil || !result.Changed || !result.HasExitCode || result.RebootRequired != (mode == "ps-reboot") {
				t.Fatalf("result=%#v error=%v", result, err)
			}
		})
	}
}
func TestPowerShellDisabledAndDirectApplyNeverLaunch(t *testing.T) {
	t.Parallel()
	p := NewPowerShell(PowerShellSettings{}, nil)
	if _, err := p.Prepare(context.Background(), plugin.Metadata{}); err == nil {
		t.Fatal("disabled plugin prepared")
	}
	if _, err := p.Apply(context.Background(), plugin.Metadata{}); err == nil {
		t.Fatal("unjournaled Apply can execute")
	}
	settings := psSettings(t, "ps-success")
	settings.Process.SHA256 = strings.Repeat("0", 64)
	if _, err := psAdapter(settings).Prepare(context.Background(), plugin.Metadata{Datasource: "nocloud"}); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("image pin was ignored: %v", err)
	}
}
func TestPowerShellExecutionCancellationIsBounded(t *testing.T) {
	t.Parallel()
	p := psAdapter(psSettings(t, "ps-hang"))
	plan, err := p.Prepare(context.Background(), plugin.Metadata{InstanceID: "fixture", Datasource: "nocloud"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = plan.Execute(ctx)
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancel error=%v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("cancelled execution did not terminate promptly")
	}
}
