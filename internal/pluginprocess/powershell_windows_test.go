//go:build windows

package pluginprocess

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/randomtoy/wininit/internal/lifecycle"
	"github.com/randomtoy/wininit/internal/platform/securefile"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/state"
	"github.com/randomtoy/wininit/plugins/datasource/configdrive"
	"github.com/randomtoy/wininit/plugins/datasource/nocloud"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestWindowsActualPowerShellExecution(t *testing.T) {
	t.Setenv("PATHEXT", ".UNTRUSTED")
	t.Setenv("ComSpec", `C:\untrusted-command-processor.exe`)
	t.Setenv("WININIT_TEST_SECRET", "fixture-private")
	native := processSettings(t, "native-exit23")
	for _, test := range []struct {
		name, body string
		exit       uint32
		failure    string
	}{
		{"success", `if('ж' -cne [string][char]0x0436){exit 18}; if([Environment]::CommandLine.Contains('fixture-private')){exit 19}; if($env:PATHEXT -cne '.COM;.EXE;.BAT;.CMD'){exit 20}; if($env:ComSpec -ine "$env:SystemRoot\System32\cmd.exe"){exit 21}; if(Test-Path Env:\WININIT_TEST_SECRET){exit 22}; [Console]::Write('fixture-private'); exit 0`, 0, ""},
		{"terminating-error", `throw 'fixture-private'`, 1, "exit code"},
		{"native-error", `& "$env:SystemRoot\System32\cmd.exe" /d /c exit 23`, 23, "exit code"},
		{"native-exe-error", `& '` + strings.ReplaceAll(native.Executable, "'", "''") + `'`, 23, "exit code"},
		{"reboot-request", `exit 3010`, 3010, ""},
		{"flood", `[Console]::Write(('fixture-private' * 10000))`, 0, "output limit"},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings := psSettings(t, "powershell")
			settings.Process.TimeoutSeconds = 60
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

// terminalStore cancels only after a terminal aggregate has reached the real
// Registry store. It lets the real long-running engine finish without polling.
type terminalStore struct {
	*state.RegistryStore
	stop  context.CancelFunc
	armed bool
}

func (s *terminalStore) Save(ctx context.Context, value state.State) error {
	if err := s.RegistryStore.Save(ctx, value); err != nil {
		return err
	}
	if value.Provisioning == "pending" || value.Provisioning == "running" {
		s.armed = true
	}
	if s.armed && (value.Provisioning == "completed" || value.Provisioning == "failed" || value.Provisioning == "reboot_required") {
		s.stop()
	}
	return nil
}

func TestWindowsPowerShellRegistryJournalAndSelectedSources(t *testing.T) {
	for _, source := range []string{"nocloud", "configdrive"} {
		t.Run(source, func(t *testing.T) {
			seed := t.TempDir()
			marker := filepath.Join(seed, "executions.txt")
			scriptPath := filepath.Join(seed, "user-data")
			metadataPath := filepath.Join(seed, "meta-data")
			var ds plugin.Datasource = nocloud.New(seed, false, nil)
			metadata := []byte("instance-id: journal-instance\n")
			if source == "configdrive" {
				version := filepath.Join(seed, "openstack", "latest")
				if err := os.MkdirAll(version, 0700); err != nil {
					t.Fatal(err)
				}
				scriptPath = filepath.Join(version, "user_data")
				metadataPath = filepath.Join(version, "meta_data.json")
				metadata = []byte(`{"uuid":"journal-instance"}`)
				ds = configdrive.New(seed, false, nil)
			}
			if err := os.WriteFile(metadataPath, metadata, 0600); err != nil {
				t.Fatal(err)
			}
			script := []byte("#ps1\n[IO.File]::AppendAllText('" + marker + "','x'); [Console]::Write('private-payload-output'); exit 0\n")
			if err := os.WriteFile(scriptPath, script, 0600); err != nil {
				t.Fatal(err)
			}
			prepareTestPath(t, seed)
			settings := psSettings(t, "powershell")
			settings.Process.TimeoutSeconds = 60
			sum := sha256.Sum256(script)
			settings.ScriptSHA256 = hex.EncodeToString(sum[:])
			registryPath := fmt.Sprintf(`SOFTWARE\WininitTests\powershell-%d`, time.Now().UnixNano())
			t.Cleanup(func() {
				_ = registry.DeleteKey(registry.LOCAL_MACHINE, registryPath+`\BootSession`)
				_ = registry.DeleteKey(registry.LOCAL_MACHINE, registryPath)
			})
			var logs bytes.Buffer
			run := func() (state.State, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				defer cancel()
				store := &terminalStore{RegistryStore: state.NewRegistryStore(registryPath), stop: cancel}
				p := NewPowerShell(settings, map[string]SourceSettings{source: {Directory: seed}})
				engine := lifecycle.NewWithPlugins(store, slog.New(slog.NewJSONHandler(&logs, nil)), plugin.Catalog{Datasources: []plugin.DatasourceRegistration{{Enabled: true, Plugin: ds}}, Provisioners: []plugin.ProvisionerRegistration{{Enabled: true, Plugin: p}}}, "native-test-config")
				err := engine.Run(ctx)
				saved, loadErr := store.Load(context.Background())
				if loadErr != nil {
					t.Fatal(loadErr)
				}
				return saved, err
			}
			saved, err := run()
			if err != nil || saved.Provisioning != "completed" || len(saved.Executions) != 1 {
				t.Fatalf("initial run=%#v error=%v", saved, err)
			}
			if err := os.Remove(scriptPath); err != nil {
				t.Fatal(err)
			}
			// Reuse must not inspect the now absent approved payload.
			saved, err = run()
			data, readErr := os.ReadFile(marker)
			if err != nil || readErr != nil || string(data) != "x" || !saved.Plugins["powershell_user_data"].Reused {
				t.Fatalf("replay or lost evidence: %#v %v %v", saved, err, readErr)
			}
			failing := []byte("#ps1\n[IO.File]::AppendAllText('" + marker + "','f'); throw 'private-payload-output'\n")
			if err := os.WriteFile(scriptPath, failing, 0600); err != nil {
				t.Fatal(err)
			}
			settings.ExecutionID = "explicit-failure"
			sum = sha256.Sum256(failing)
			settings.ScriptSHA256 = hex.EncodeToString(sum[:])
			if _, err := run(); err == nil {
				t.Fatal("terminating failure reported success")
			}
			if _, err := run(); err == nil {
				t.Fatal("failed consumed ID retried")
			}
			data, _ = os.ReadFile(marker)
			if string(data) != "xf" {
				t.Fatal("failed script was executed more than once")
			}
			reboot := []byte("#ps1\nexit 3010\n")
			if err := os.WriteFile(scriptPath, reboot, 0600); err != nil {
				t.Fatal(err)
			}
			settings.ExecutionID = "request-only"
			sum = sha256.Sum256(reboot)
			settings.ScriptSHA256 = hex.EncodeToString(sum[:])
			for i := 0; i < 2; i++ {
				saved, err = run()
				if err != nil || saved.Provisioning != "reboot_required" {
					t.Fatalf("service restart satisfied reboot: %#v %v", saved, err)
				}
			}
			encoded, _ := json.Marshal(saved)
			if strings.Contains(logs.String(), "private-payload-output") || bytes.Contains(encoded, []byte("private-payload-output")) {
				t.Fatal("payload/output escaped into state or logs")
			}
		})
	}
}

func TestWindowsPowerShellInterpreterImageBoundary(t *testing.T) {
	image, err := securefile.TrustedSystemPowerShell()
	if err != nil {
		t.Fatalf("native OS PowerShell image verification: %v", err)
	}
	defer image.Close()
}

func TestWindowsPowerShellCancellationKillsNativeDescendant(t *testing.T) {
	settings := psSettings(t, "powershell")
	settings.Process.TimeoutSeconds = 60
	seed := t.TempDir()
	pidFile := filepath.Join(seed, "child.pid")
	body := `$child=Start-Process -FilePath "$env:SystemRoot\System32\WindowsPowerShell\v1.0\powershell.exe" -ArgumentList '-NoLogo','-NoProfile','-NonInteractive','-Command','Start-Sleep -Seconds 60' -NoNewWindow -PassThru; [IO.File]::WriteAllText('` + pidFile + `',[string]$child.Id); Start-Sleep -Seconds 60`
	script := []byte("#ps1\n" + body + "\n")
	for name, data := range map[string][]byte{"meta-data": []byte("instance-id: cancellation\n"), "user-data": script} {
		if err := os.WriteFile(filepath.Join(seed, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	prepareTestPath(t, seed)
	sum := sha256.Sum256(script)
	settings.ScriptSHA256 = hex.EncodeToString(sum[:])
	p := NewPowerShell(settings, map[string]SourceSettings{"nocloud": {Directory: seed}})
	plan, err := p.Prepare(context.Background(), plugin.Metadata{InstanceID: "cancellation", Datasource: "nocloud"})
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := plan.Execute(ctx); done <- err }()
	var pid uint64
	until := time.Now().Add(45 * time.Second)
	for time.Now().Before(until) {
		if data, err := os.ReadFile(pidFile); err == nil {
			pid, _ = strconv.ParseUint(string(data), 10, 32)
			if pid != 0 {
				break
			}
		}
		select {
		case err := <-done:
			t.Fatalf("script failed before native descendant: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
	if pid == 0 {
		t.Fatal("native descendant did not start")
	}
	process, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(process)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("unexpected cancellation outcome: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Execute did not honor cancellation")
	}
	plan.Close()
	result, err := windows.WaitForSingleObject(process, 5000)
	if err != nil || result != windows.WAIT_OBJECT_0 {
		t.Fatal("native descendant survived cancelled job", result, err)
	}
}
