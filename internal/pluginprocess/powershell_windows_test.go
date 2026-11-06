//go:build windows

package pluginprocess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/randomtoy/wininit/internal/plugin"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsActualPowerShellExecution(t *testing.T) {
	for _, test := range []struct {
		name, body string
		exit       uint32
		failure    string
	}{
		{"success", `if([Environment]::CommandLine.Contains('fixture-private')){exit 19}; [Console]::Write('fixture-private'); exit 0`, 0, ""},
		{"terminating-error", `throw 'fixture-private'`, 1, "exit code"},
		{"native-error", `& "$env:SystemRoot\System32\cmd.exe" /c exit 23`, 23, "exit code"},
		{"reboot-request", `exit 3010`, 3010, ""},
		{"flood", `[Console]::Write(('fixture-private' * 10000))`, 0, "output limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := psSettings(t, "powershell")
			seed := t.TempDir()
			if err := os.WriteFile(filepath.Join(seed, "meta-data"), []byte("instance-id: native-instance\n"), 0600); err != nil {
				t.Fatal(err)
			}
			script := []byte("#ps1_sysnative\n" + test.body + "\n")
			sum := sha256.Sum256(script)
			settings.ScriptSHA256 = hex.EncodeToString(sum[:])
			if err := os.WriteFile(filepath.Join(seed, "user-data"), script, 0600); err != nil {
				t.Fatal(err)
			}
			prepareTestPath(t, seed)
			p := NewPowerShell(settings, map[string]SourceSettings{"nocloud": {Directory: seed}})
			plan, err := p.Prepare(context.Background(), plugin.Metadata{InstanceID: "native-instance", Datasource: "nocloud"})
			if err != nil {
				t.Fatal(err)
			}
			defer plan.Close()
			result, err := plan.Execute(context.Background())
			if test.failure != "" {
				if err == nil || !strings.Contains(err.Error(), test.failure) || strings.Contains(err.Error(), "fixture-private") {
					t.Fatalf("unsafe failure: %#v %v", result, err)
				}
				return
			}
			if err != nil || !result.HasExitCode || result.ExitCode != test.exit || result.RebootRequired != (test.exit == 3010) {
				t.Fatalf("native execution result=%#v error=%v", result, err)
			}
		})
	}
}
