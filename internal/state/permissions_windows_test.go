//go:build windows

package state

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func registryTestACL(t *testing.T, key registry.Key, descriptor string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetSecurityInfo(windows.Handle(key), windows.SE_REGISTRY_KEY, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRejectsWritableJournalAndAncestors(t *testing.T) {
	for _, ancestor := range []bool{false, true} {
		t.Run(fmt.Sprintf("ancestor-%t", ancestor), func(t *testing.T) {
			parent := fmt.Sprintf(`SOFTWARE\WininitTests\permissions-%d`, time.Now().UnixNano())
			path := parent + `\State`
			t.Cleanup(func() {
				_ = registry.DeleteKey(registry.LOCAL_MACHINE, path)
				_ = registry.DeleteKey(registry.LOCAL_MACHINE, parent)
			})
			store := NewRegistryStore(path)
			value := Initial(time.Now())
			now := time.Now().UTC()
			value.Executions = map[string]Execution{strings.Repeat("a", 64): {ApprovalHash: strings.Repeat("b", 64), BootID: strings.Repeat("c", 64), Status: "failed", FailureCode: "exit_failure", HasExitCode: true, ExitCode: 7, StartedAt: now, FinishedAt: now}}
			if err := store.Save(context.Background(), value); err != nil {
				t.Fatal(err)
			}
			key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.ALL_ACCESS)
			if err != nil {
				t.Fatal(err)
			}
			defer key.Close()
			target := key
			if ancestor {
				target, err = registry.OpenKey(registry.LOCAL_MACHINE, parent, registry.ALL_ACCESS)
				if err != nil {
					t.Fatal(err)
				}
				defer target.Close()
			}
			// A valid checksum does not authenticate a journal. An ordinary writer can
			// clear consumed intent and recompute the envelope to authorize replay.
			registryTestACL(t, target, "O:BAG:BAD:P(A;;KA;;;SY)(A;;KA;;;BA)(A;;KA;;;WD)")
			tampered := value
			tampered.Executions = nil
			active, _, err := key.GetStringValue(activeValue)
			if err != nil {
				t.Fatal(err)
			}
			if err := key.SetBinaryValue(active, rawCheckpoint(t, tampered)); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(context.Background()); err == nil {
				t.Fatal("accepted a replay-enabling journal from an untrusted writer")
			}
			if err := store.Save(context.Background(), value); err == nil {
				t.Fatal("saved into a journal path replaceable by an untrusted writer")
			}
		})
	}
}

func TestRegistryTrustedLegacyCheckpointStillMigrates(t *testing.T) {
	path := fmt.Sprintf(`SOFTWARE\WininitTests\trusted-%d`, time.Now().UnixNano())
	t.Cleanup(func() { _ = registry.DeleteKey(registry.LOCAL_MACHINE, path) })
	store := NewRegistryStore(path)
	old := Initial(time.Now())
	old.SchemaVersion = 2
	old.Provisioning = "completed"
	old.Plugins = map[string]PluginState{"hostname": {Status: "completed"}}
	// Create a legacy-style key explicitly; Load must validate, not rewrite its
	// ACL or invent an execution journal while migrating the old payload.
	key, _, err := registry.CreateKey(registry.LOCAL_MACHINE, path, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	registryTestACL(t, key, "O:BAG:BAD:P(A;;KA;;;SY)(A;;KA;;;BA)(A;;KR;;;BU)")
	if err := key.SetBinaryValue(slotAValue, rawCheckpoint(t, old)); err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue(activeValue, slotAValue); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background())
	if err != nil || got.SchemaVersion != CurrentSchemaVersion || got.Plugins["hostname"].Status != "completed" || len(got.Executions) != 0 {
		t.Fatalf("trusted migration=%#v error=%v", got, err)
	}
	if err := store.Save(context.Background(), got); err != nil {
		t.Fatal(err)
	}
}
