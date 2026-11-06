//go:build windows

package securefile

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"testing"
)

func TestDirectoryHandlePinsAgainstRename(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "pinned")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := OpenLocal(directory, windows.READ_CONTROL|windows.FILE_LIST_DIRECTORY, windows.FILE_DIRECTORY_FILE)
	if err != nil {
		t.Fatal(err)
	}
	moved := directory + "-moved"
	if err := os.Rename(directory, moved); err == nil {
		file.Close()
		t.Fatal("opened directory could be renamed")
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(directory, moved); err != nil {
		t.Fatalf("released directory remained pinned: %v", err)
	}
}

func TestVerifiedExecutablePinsAllAncestors(t *testing.T) {
	ancestor := filepath.Join(t.TempDir(), "ancestor")
	parent := filepath.Join(ancestor, "protected")
	if err := os.MkdirAll(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "image.exe")
	if err := os.WriteFile(path, []byte("verified-image"), 0o600); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := sd.Owner()
	dacl, _, _ := sd.DACL()
	for _, target := range []string{parent, path} {
		if err := windows.SetNamedSecurityInfo(target, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil); err != nil {
			t.Fatalf("protect executable fixture: %v", err)
		}
	}
	image, err := TrustedExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	for _, directory := range []string{parent, ancestor} {
		if err := os.Rename(directory, directory+"-moved"); err == nil {
			t.Fatal("verified executable path could be redirected by renaming a directory")
		}
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err == nil {
		t.Fatal("verified executable could be overwritten")
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(ancestor, ancestor+"-moved"); err != nil {
		t.Fatalf("released ancestor remained pinned: %v", err)
	}
}

func TestRejectWin32AliasesBeforeOpening(t *testing.T) {
	for _, path := range []string{
		`C:\protected\image.exe:stream`, `C:\protected\image.exe.`, `C:\protected\image.exe `,
		`C:\protected.\image.exe`, `C:\protected\..\image.exe`, `C:\protected\NUL.exe`,
		`C:\protected\COM1.exe`, `\\server\share\image.exe`, `\\?\C:\image.exe`,
	} {
		if checkLocalPath(path) == nil {
			t.Fatalf("ambiguous path accepted: %q", path)
		}
		if _, err := TrustedFile(path, false); err == nil {
			t.Fatalf("alias reached the filesystem: %q", path)
		}
	}
	for _, path := range []string{`C:\Program Files\WinInit\plugin.exe`, `D:\`, `C:\Users\worker\.ssh\authorized_keys`} {
		if err := checkLocalPath(path); err != nil {
			t.Fatalf("ordinary path rejected: %v", err)
		}
	}
}
