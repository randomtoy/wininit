//go:build windows

package pluginprocess

import (
	"context"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func prepareTestPath(t *testing.T, root string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;GRGX;;;BU)")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := sd.Owner()
	group, _, _ := sd.Group()
	dacl, _, _ := sd.DACL()
	err = filepath.Walk(root, func(path string, _ os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, group, dacl, nil)
	})
	if err != nil {
		t.Fatalf("prepare protected readable test path: %v", err)
	}
}

func TestWindowsRestrictedProcessCannotReadSecretsWriteOrSpawn(t *testing.T) {
	settings := processSettings(t, "restrictions")
	directory := t.TempDir()
	prepareTestPath(t, directory)
	secret := filepath.Join(directory, "host-secret")
	if err := os.WriteFile(secret, []byte("fixture-host-private"), 0o600); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := sd.Owner()
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(secret, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	got, err := NewConfigDrive(settings, directory, false).Discover(context.Background())
	if err != nil || got.InstanceID != "fixture-instance" {
		t.Fatalf("native restrictions were not enforced: %#v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "forbidden-write")); !os.IsNotExist(err) {
		t.Fatal("restricted child published a file")
	}
}
