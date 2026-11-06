//go:build windows && integration

package identity

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/platform/securefile"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/plugins/provisioner/authorizedkeys"
	"github.com/randomtoy/wininit/plugins/provisioner/localusers"
	"github.com/randomtoy/wininit/plugins/provisioner/passwordpolicy"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/windows"
)

func protectedTestPath(t *testing.T, path, extra string) {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)" + extra)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, _ := sd.Owner()
	dacl, _, _ := sd.DACL()
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, owner, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
}
func requireElevated(t *testing.T) {
	t.Helper()
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Fatal("isolated identity integration tests require an elevated disposable Windows runner")
	}
}
func TestProtectedPasswordFiles(t *testing.T) {
	requireElevated(t)
	ctx := context.Background()
	adapter := New()
	directory := t.TempDir()
	protectedTestPath(t, directory, "")
	path := filepath.Join(directory, "password")
	secret := []byte("fixture-protected-password\r\n")
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	protectedTestPath(t, path, "")
	data, err := adapter.ReadPassword(ctx, path)
	if err != nil || string(data) != "fixture-protected-password" {
		t.Fatalf("protected password read failed: %v", err)
	}
	clear(data)
	stream := path + ":hidden"
	if err := os.WriteFile(stream, []byte("fixture-stream-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ReadPassword(ctx, stream); err == nil {
		t.Fatal("password alternate data stream accepted")
	}
	protectedTestPath(t, path, "(A;;GR;;;BU)")
	if _, err := adapter.ReadPassword(ctx, path); err == nil || strings.Contains(err.Error(), "fixture-protected") {
		t.Fatal("readable password file accepted or leaked")
	}
	protectedTestPath(t, path, "")
	link := filepath.Join(directory, "hardlink")
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.ReadPassword(ctx, path); err == nil {
		t.Fatal("hard-linked password accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	protectedTestPath(t, directory, "(A;OICI;GW;;;BU)")
	if _, err := adapter.ReadPassword(ctx, path); err == nil {
		t.Fatal("writable password parent accepted")
	}
}
func TestNativeUsersKeysRerunRevocationAndUnsafePaths(t *testing.T) {
	requireElevated(t)
	ctx := context.Background()
	adapter := New()
	random := make([]byte, 8)
	if _, err := rand.Read(random); err != nil {
		t.Fatal(err)
	}
	name := "wi" + hex.EncodeToString(random)
	desired := identity.User{Name: name, FullName: "WinInit isolated test", Description: "Owned by wininit integration test"}
	secretDir := t.TempDir()
	protectedTestPath(t, secretDir, "")
	desired.PasswordFile = filepath.Join(secretDir, "initial-password")
	password := []byte("Wi1!" + hex.EncodeToString(random) + "-StrongFixture")
	if err := os.WriteFile(desired.PasswordFile, password, 0o600); err != nil {
		t.Fatal(err)
	}
	protectedTestPath(t, desired.PasswordFile, "")
	var sid string
	t.Cleanup(func() {
		if _, err := existingProfile(sid); sid != "" && err == nil {
			encoded, _ := windows.UTF16PtrFromString(sid)
			deleted, _, _ := windows.NewLazySystemDLL("userenv.dll").NewProc("DeleteProfileW").Call(uintptr(unsafe.Pointer(encoded)), 0, 0)
			if deleted == 0 {
				t.Error("isolated test profile cleanup failed")
			}
		}
		encoded, _ := windows.UTF16PtrFromString(name)
		code, _, _ := netapi.NewProc("NetUserDel").Call(0, uintptr(unsafe.Pointer(encoded)))
		if code != 0 && code != 2221 {
			t.Errorf("isolated test account cleanup failed (code %d)", code)
		}
		clear(password)
	})
	users := localusers.New([]identity.User{desired}, adapter)
	result, err := users.Apply(ctx, plugin.Metadata{})
	if err != nil || !result.Changed {
		t.Fatalf("create account: %#v %v", result, err)
	}
	info, cleanup, err := readUser(ctx, name)
	if err != nil || info == nil {
		t.Fatalf("read created account: %v", err)
	}
	sid = info.SID.String()
	cleanup()
	// The secret can be removed immediately after creation. A repeated run and
	// attribute update must still succeed and preserve the initial password.
	if err := os.Remove(desired.PasswordFile); err != nil {
		t.Fatal(err)
	}
	result, err = users.Apply(ctx, plugin.Metadata{})
	if err != nil || result.Changed {
		t.Fatalf("account rerun: %#v %v", result, err)
	}
	desired.Description = "Updated isolated test description"
	result, err = localusers.New([]identity.User{desired}, adapter).Apply(ctx, plugin.Metadata{})
	if err != nil || !result.Changed {
		t.Fatalf("account attributes: %#v %v", result, err)
	}
	encodedName, _ := windows.UTF16PtrFromString(name)
	domain, _ := windows.UTF16PtrFromString(".")
	encodedPassword, _ := windows.UTF16FromString(string(password))
	defer clear(encodedPassword)
	var token windows.Token
	ok, _, _ := windows.NewLazySystemDLL("advapi32.dll").NewProc("LogonUserW").Call(uintptr(unsafe.Pointer(encodedName)), uintptr(unsafe.Pointer(domain)), uintptr(unsafe.Pointer(&encodedPassword[0])), 3, 0, uintptr(unsafe.Pointer(&token)))
	if ok == 0 {
		t.Fatal("initial password did not survive account reconciliation")
	}
	token.Close()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	settings := []identity.KeyUser{{Name: name, PublicKeys: []string{line}}}
	keys := authorizedkeys.New(settings, adapter)
	result, err = keys.Apply(ctx, plugin.Metadata{})
	if err != nil || !result.Changed {
		t.Fatalf("SSH key apply: %#v %v", result, err)
	}
	profile, err := existingProfile(sid)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(profile, ".ssh", "authorized_keys")
	file, err := securefile.OpenLocal(path, windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_NON_DIRECTORY_FILE)
	if err != nil {
		t.Fatal(err)
	}
	matches, err := keyACLMatches(windows.Handle(file.Fd()), sid)
	file.Close()
	if err != nil || !matches {
		t.Fatalf("SSH key ACL is not protected: %v", err)
	}
	result, err = keys.Apply(ctx, plugin.Metadata{})
	if err != nil || result.Changed {
		t.Fatalf("SSH key rerun: %#v %v", result, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unmanaged := []byte("# operator-owned key material\r\n")
	if err := os.WriteFile(path, append(append([]byte{}, unmanaged...), data...), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = authorizedkeys.New([]identity.KeyUser{{Name: name}}, adapter).Apply(ctx, plugin.Metadata{})
	if err != nil || !result.Changed {
		t.Fatalf("SSH managed key revocation: %#v %v", result, err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, unmanaged) {
		t.Fatal("revocation changed unmanaged keys")
	}
	target := filepath.Join(secretDir, "outside")
	if err := os.WriteFile(target, []byte("outside-fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, path); err != nil {
		t.Fatal(err)
	}
	if _, err := keys.Apply(ctx, plugin.Metadata{}); err == nil {
		t.Fatal("hard-linked authorized_keys accepted")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	t.Run("symlink", func(t *testing.T) {
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlink privilege unavailable: %v", err)
		}
		defer os.Remove(path)
		if _, err := keys.Apply(ctx, plugin.Metadata{}); err == nil {
			t.Fatal("symlinked authorized_keys accepted")
		}
	})
	got, err = os.ReadFile(target)
	if err != nil || string(got) != "outside-fixture" {
		t.Fatal("outside file was modified")
	}
}
func TestNativePasswordPolicyRoundTripAndIdempotence(t *testing.T) {
	requireElevated(t)
	ctx := context.Background()
	if err := localOnly(ctx); err != nil {
		t.Fatal(err)
	}
	var originalBase modals0
	var originalLockout modals3
	if err := readModals(0, unsafe.Pointer(&originalBase), unsafe.Sizeof(originalBase)); err != nil {
		t.Fatal(err)
	}
	if err := readModals(3, unsafe.Pointer(&originalLockout), unsafe.Sizeof(originalLockout)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, field := range []struct {
			level uintptr
			value unsafe.Pointer
		}{{0, unsafe.Pointer(&originalBase)}, {3, unsafe.Pointer(&originalLockout)}} {
			var parameter uint32
			code, _, _ := netUserModalsSet.Call(0, field.level, uintptr(field.value), uintptr(unsafe.Pointer(&parameter)))
			if code != 0 {
				t.Errorf("restore isolated runner password policy failed (code %d)", code)
			}
		}
	})
	desired := identity.DefaultPasswordPolicy()
	adapter := New()
	current, err := adapter.PasswordPolicy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current == desired {
		desired.HistoryLength = 23
	}
	p := passwordpolicy.New(desired, adapter)
	result, err := p.Apply(ctx, plugin.Metadata{})
	if err != nil || !result.Changed {
		t.Fatalf("native password policy change: %#v %v", result, err)
	}
	current, err = adapter.PasswordPolicy(ctx)
	if err != nil || current != desired {
		t.Fatalf("policy readback=%#v %v", current, err)
	}
	result, err = p.Apply(ctx, plugin.Metadata{})
	if err != nil || result.Changed {
		t.Fatalf("native policy rerun: %#v %v", result, err)
	}
}
