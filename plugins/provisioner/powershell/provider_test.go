package powershell

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
)

type executorFixture struct {
	calls  int
	script []byte
	seen   []byte
}

func (e *executorFixture) Run(_ context.Context, script []byte, _ uint32) (*userdatav1.Execution, error) {
	e.calls++
	e.script = script
	e.seen = bytes.Clone(script)
	return &userdatav1.Execution{Outcome: userdatav1.Outcome_SUCCESS}, nil
}
func sourceFixture(t *testing.T, kind, script string) *userdatav1.PrepareRequest {
	t.Helper()
	root := t.TempDir()
	selected := root
	metaName, dataName, meta := "meta-data", "user-data", "instance-id: fixture-instance\nlocal-hostname: fixture\n"
	if kind == "configdrive" {
		selected = filepath.Join(root, "openstack", "latest")
		metaName, dataName, meta = "meta_data.json", "user_data", `{"uuid":"fixture-instance","hostname":"FIXTURE.example.invalid"}`
		if err := os.MkdirAll(selected, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(selected, metaName), []byte(meta), 0600); err != nil {
		t.Fatal(err)
	}
	if script != "" {
		if err := os.WriteFile(filepath.Join(selected, dataName), []byte(script), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte(script))
	return &userdatav1.PrepareRequest{Datasource: kind, SeedDirectory: root, ExpectedInstanceId: "fixture-instance", ExpectedHostname: "fixture", ApprovedSha256: hex.EncodeToString(sum[:])}
}
func TestPrepareSnapshotsApprovedSelectedPayloadAndExecuteConsumesItOnce(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"nocloud", "configdrive"} {
		t.Run(kind, func(t *testing.T) {
			executor := &executorFixture{}
			provider := New(executor)
			request := sourceFixture(t, kind, "#ps1_sysnative\r\nWrite-Output 'fixture-private'\r\n")
			prepared, err := provider.Prepare(context.Background(), request)
			if err != nil || executor.calls != 0 || prepared.GetLeaseId() == "" {
				t.Fatalf("prepare executed code or failed: %v", err)
			}
			file := filepath.Join(request.SeedDirectory, "user-data")
			if kind == "configdrive" {
				file = filepath.Join(request.SeedDirectory, "openstack", "latest", "user_data")
			}
			if err := os.WriteFile(file, []byte("unapproved replacement"), 0600); err != nil {
				t.Fatal(err)
			}
			result, err := provider.Execute(context.Background(), &userdatav1.ExecuteRequest{LeaseId: prepared.LeaseId, MaxOutputBytes: 1024})
			if err != nil || result.GetOutcome() != userdatav1.Outcome_SUCCESS || executor.calls != 1 {
				t.Fatal(err)
			}
			if !bytes.Contains(executor.seen, []byte("fixture-private")) {
				t.Fatal("prepared script was replaced between approval and execution")
			}
			if !bytes.Equal(executor.script, make([]byte, len(executor.script))) {
				t.Fatal("owned script buffer was not cleared")
			}
			if _, err := provider.Execute(context.Background(), &userdatav1.ExecuteRequest{LeaseId: prepared.LeaseId, MaxOutputBytes: 1024}); err == nil || executor.calls != 1 {
				t.Fatal("lease replayed")
			}
		})
	}
}
func TestRejectedPayloadsNeverExecute(t *testing.T) {
	t.Parallel()
	for _, script := range []string{"#cloud-config\npassword: fixture-private", "#ps1_x86\nexit 0", "#!/bin/sh\ntrue", "#ps1\n", "#ps1\n\x00", "#ps1\n\xff", strings.Repeat("x", userdatav1.MaxScriptBytes+1)} {
		p := New(&executorFixture{})
		request := sourceFixture(t, "nocloud", script)
		if _, err := p.Prepare(context.Background(), request); err == nil || strings.Contains(err.Error(), "fixture-private") {
			t.Fatal("unsupported or malformed input accepted/leaked")
		}
	}
	request := sourceFixture(t, "nocloud", "#ps1\nexit 0\n")
	request.ApprovedSha256 = strings.Repeat("0", 64)
	if _, err := New(nil).Prepare(context.Background(), request); !errors.Is(err, userdatav1.ErrApproval) {
		t.Fatal("unapproved script was accepted")
	}
}
func TestSourceBindingMissingDataAndNoVersionDowngrade(t *testing.T) {
	t.Parallel()
	request := sourceFixture(t, "nocloud", "")
	result, err := New(nil).Prepare(context.Background(), request)
	if err != nil || !result.Unavailable {
		t.Fatal("missing payload did not skip")
	}
	request = sourceFixture(t, "nocloud", "#ps1\nexit 0\n")
	request.ExpectedInstanceId = "different-instance"
	if _, err := New(nil).Prepare(context.Background(), request); !errors.Is(err, userdatav1.ErrSource) {
		t.Fatal("changed source identity accepted")
	}
	request = sourceFixture(t, "configdrive", "#ps1\nexit 0\n")
	newer := filepath.Join(request.SeedDirectory, "openstack", "2025-04-04")
	if err := os.Mkdir(newer, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(nil).Prepare(context.Background(), request); !errors.Is(err, userdatav1.ErrSource) {
		t.Fatal("malformed selected version silently downgraded")
	}
}
func TestLinksAndPathInjectionAreRejected(t *testing.T) {
	t.Parallel()
	for _, link := range []string{"hard", "symbolic"} {
		t.Run(link, func(t *testing.T) {
			request := sourceFixture(t, "nocloud", "#ps1\nexit 0\n")
			path := filepath.Join(request.SeedDirectory, "user-data")
			target := filepath.Join(t.TempDir(), "outside")
			if err := os.Rename(path, target); err != nil {
				t.Fatal(err)
			}
			var err error
			if link == "hard" {
				err = os.Link(target, path)
			} else {
				err = os.Symlink(target, path)
			}
			if err != nil {
				t.Skipf("link creation unavailable: %v", err)
			}
			if _, err := New(nil).Prepare(context.Background(), request); err == nil {
				t.Fatal("linked script accepted")
			}
		})
	}
	request := sourceFixture(t, "nocloud", "#ps1\nexit 0\n")
	request.Datasource = "../powershell -Command arbitrary"
	if _, err := New(nil).Prepare(context.Background(), request); !errors.Is(err, userdatav1.ErrSource) {
		t.Fatal("datasource injection accepted")
	}
}
func FuzzScriptEnvelope(f *testing.F) {
	f.Add([]byte("#ps1\nexit 0\n"))
	f.Add([]byte("#cloud-config\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		body, err := parseScript(data)
		if err == nil && (len(body) == 0 || !bytes.Contains(data, body)) {
			t.Fatal("parser synthesized executable data")
		}
	})
}
