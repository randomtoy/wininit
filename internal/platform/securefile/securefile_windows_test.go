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
