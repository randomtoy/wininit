//go:build windows

package state

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var regCreateProtectedKey = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegCreateKeyExW")

// openStateKey authenticates the storage boundary before trusting checkpoints.
// Valid JSON/checksums alone cannot prevent an ordinary Registry writer from
// removing consumed execution IDs. Validate ancestors as well: an attacker
// able to rename/replace an ancestor could substitute an older journal.
func (s *RegistryStore) openStateKey(ctx context.Context, access uint32, create bool) (registry.Key, error) {
	if s.root != registry.LOCAL_MACHINE {
		// HKCU is used only by isolated, non-production storage tests.
		if create {
			key, _, err := registry.CreateKey(s.root, s.path, access)
			return key, err
		}
		return registry.OpenKey(s.root, s.path, access)
	}
	parts := strings.Split(s.path, `\`)
	if len(parts) < 2 || !strings.EqualFold(parts[0], "SOFTWARE") {
		return 0, ErrUnsafePermissions
	}
	for _, part := range parts {
		if part == "" || strings.ContainsAny(part, "/\x00") {
			return 0, ErrUnsafePermissions
		}
	}
	parent := s.root
	owned := false
	closeParent := func() {
		if owned {
			parent.Close()
		}
	}
	for index, part := range parts {
		if err := ctx.Err(); err != nil {
			closeParent()
			return 0, err
		}
		rights := uint32(registry.READ)
		if index == len(parts)-1 {
			rights = access | windows.READ_CONTROL
		}
		key, err := registry.OpenKey(parent, part, rights)
		if errors.Is(err, registry.ErrNotExist) && create {
			key, err = createProtectedKey(parent, part, rights)
		}
		closeParent()
		if err != nil {
			return 0, err
		}
		parent, owned = key, true
		if err := trustedRegistryKey(key); err != nil {
			closeParent()
			return 0, err
		}
	}
	return parent, nil
}

func createProtectedKey(parent registry.Key, name string, access uint32) (registry.Key, error) {
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;CI;KA;;;SY)(A;CI;KA;;;BA)")
	if err != nil {
		return 0, err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	encoded, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	var key windows.Handle
	code, _, _ := regCreateProtectedKey.Call(uintptr(parent), uintptr(unsafe.Pointer(encoded)), 0, 0, 0, uintptr(access), uintptr(unsafe.Pointer(&attributes)), uintptr(unsafe.Pointer(&key)), 0)
	if code != 0 {
		return 0, syscall.Errno(code)
	}
	return registry.Key(key), nil
}

func trustedRegistryKey(key registry.Key) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(key), windows.SE_REGISTRY_KEY, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("inspect Registry state security: %w", err)
	}
	trusted := func(sid *windows.SID) bool {
		// Windows servicing may own ancestors such as HKLM\SOFTWARE.
		return sid != nil && (sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) || sid.String() == "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464")
	}
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return fmt.Errorf("%w: untrusted key owner", ErrUnsafePermissions)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil {
		return fmt.Errorf("%w: key requires an explicit DACL", ErrUnsafePermissions)
	}
	const mutation = uint32(windows.GENERIC_WRITE | windows.GENERIC_ALL | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER | registry.SET_VALUE | registry.CREATE_SUB_KEY | registry.CREATE_LINK)
	for i := uint16(0); i < acl.AceCount; i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, uint32(i), &ace); err != nil {
			return err
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 || ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return fmt.Errorf("%w: unsupported access entry", ErrUnsafePermissions)
		}
		if uint32(ace.Mask)&mutation != 0 && !trusted((*windows.SID)(unsafe.Pointer(&ace.SidStart))) {
			return fmt.Errorf("%w: key or ancestor permits untrusted mutation", ErrUnsafePermissions)
		}
	}
	return nil
}
