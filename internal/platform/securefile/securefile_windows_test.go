//go:build windows

package securefile

import "testing"

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
