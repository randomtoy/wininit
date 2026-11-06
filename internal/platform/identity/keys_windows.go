//go:build windows

package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/platform/securefile"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var createProfile = windows.NewLazySystemDLL("userenv.dll").NewProc("CreateProfile")
var setSecurityObject = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtSetSecurityObject")

const fileAddSubdirectory = 0x0004
const fileDeleteChild = 0x0040

func profilePath(ctx context.Context, user, sid string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	path, err := existingProfile(sid)
	if err == nil {
		return path, nil
	}
	if !errors.Is(err, registry.ErrNotExist) {
		return "", errors.New("read local user profile failed")
	}
	encodedSID, _ := windows.UTF16PtrFromString(sid)
	encodedUser, _ := windows.UTF16PtrFromString(user)
	// CreateProfile's RPC contract is bounded to MAX_PATH, even on systems
	// supporting longer ordinary filesystem paths.
	buffer := make([]uint16, windows.MAX_PATH)
	result, _, _ := createProfile.Call(uintptr(unsafe.Pointer(encodedSID)), uintptr(unsafe.Pointer(encodedUser)), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if result != 0 && uint32(result) != 0x800700b7 {
		return "", fmt.Errorf("create local user profile failed (HRESULT 0x%x)", uint32(result))
	}
	path, err = existingProfile(sid)
	if err != nil {
		return "", errors.New("read created local user profile failed")
	}
	return path, nil
}

func existingProfile(sid string) (string, error) {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList\`+sid, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "", err
	}
	defer key.Close()
	path, kind, err := key.GetStringValue("ProfileImagePath")
	if err != nil {
		return "", err
	}
	if kind == registry.EXPAND_SZ {
		path, err = registry.ExpandString(path)
		if err != nil {
			return "", err
		}
	}
	if !identity.LocalAbsolutePath(path) {
		return "", errors.New("user profile must have an absolute local path")
	}
	return path, nil
}

// ReconcileKeys never follows a reparse point or edits a pre-existing file's
// ACL through a possibly hard-linked name. Open directory handles pin paths.
// Replacements are created with their final protected DACL before publication.
func (*Adapter) ReconcileKeys(ctx context.Context, user string, keys []string) (bool, error) {
	info, cleanup, err := readUser(ctx, user)
	if err != nil {
		return false, err
	}
	defer cleanup()
	if info == nil {
		return false, errors.New("SSH keys require an existing standard local account")
	}
	sid := info.SID.String()
	path, err := profilePath(ctx, user, sid)
	if err != nil {
		return false, err
	}
	profile, err := securefile.OpenLocal(path, windows.FILE_LIST_DIRECTORY|fileAddSubdirectory|windows.READ_CONTROL, windows.FILE_DIRECTORY_FILE)
	if err != nil {
		return false, fmt.Errorf("open local user profile without reparse points: %w", err)
	}
	defer profile.Close()
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + sid + ")")
	if err != nil {
		return false, err
	}
	sshDir, err := securefile.OpenRelative(windows.Handle(profile.Fd()), ".ssh", windows.FILE_GENERIC_READ|windows.FILE_GENERIC_WRITE|fileDeleteChild|windows.READ_CONTROL|windows.WRITE_DAC, windows.FILE_OPEN_IF, windows.FILE_DIRECTORY_FILE, sd)
	if err != nil {
		return false, fmt.Errorf("open or create .ssh without reparse points: %w", err)
	}
	defer sshDir.Close()
	folderChanged, err := ensureKeyDirectoryACL(windows.Handle(sshDir.Fd()), sid, sd)
	if err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	existing, aclMatches, err := readKeys(windows.Handle(sshDir.Fd()), sid)
	if err != nil {
		return false, err
	}
	merged, err := identity.MergeKeys(existing, keys)
	if err != nil {
		return false, err
	}
	if bytes.Equal(existing, merged) && aclMatches {
		return folderChanged, nil
	}
	// Avoid creating an empty file solely to revoke a block which never existed.
	if len(existing) == 0 && len(merged) == 0 && !aclMatches {
		return folderChanged, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := replaceKeys(ctx, windows.Handle(sshDir.Fd()), merged, sd); err != nil {
		return false, err
	}
	return true, nil
}

func keyACLMatches(handle windows.Handle, sid string) (bool, error) {
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, errors.New("inspect SSH key security failed")
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false, errors.New("inspect SSH key owner failed")
	}
	if owner.String() != sid && !owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return false, nil
	}
	control, _, err := sd.Control()
	if err != nil {
		return false, err
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		return false, nil
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return false, errors.New("SSH keys require an access-control list")
	}
	seen := map[string]bool{}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return false, err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			return false, nil
		}
		principal := (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()
		if principal != sid && principal != "S-1-5-18" && principal != "S-1-5-32-544" {
			return false, nil
		}
		if ace.Mask != 0x1f01ff {
			return false, nil
		}
		seen[principal] = true
	}
	return len(seen) == 3, nil
}

func ensureKeyDirectoryACL(handle windows.Handle, sid string, desired *windows.SECURITY_DESCRIPTOR) (bool, error) {
	matches, err := keyACLMatches(handle, sid)
	if err != nil {
		return false, err
	}
	if matches {
		return false, nil
	}
	actual, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	owner, _, err := actual.Owner()
	if err != nil || owner == nil || owner.String() != sid && !owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return false, errors.New(".ssh directory has an unsupported owner")
	}
	// SetSecurityInfo propagates inheritable ACEs through existing children.
	// A user's .ssh may contain hard links to unrelated files, so update only
	// this opened directory object and never traverse its children for ACLs.
	status, _, _ := setSecurityObject.Call(uintptr(handle), uintptr(windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION), uintptr(unsafe.Pointer(desired)))
	if status != 0 {
		return false, fmt.Errorf("protect .ssh directory permissions: %w", windows.NTStatus(status))
	}
	return true, nil
}

func readKeys(parent windows.Handle, sid string) ([]byte, bool, error) {
	file, err := securefile.OpenRelative(parent, "authorized_keys", windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("open authorized_keys without reparse points: %w", err)
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil || info.NumberOfLinks != 1 {
		return nil, false, errors.New("authorized_keys must be a regular file with exactly one hard link")
	}
	data, err := io.ReadAll(io.LimitReader(file, identity.MaxAuthorizedKeysBytes+1))
	if err != nil {
		return nil, false, errors.New("read authorized_keys failed")
	}
	matches, err := keyACLMatches(windows.Handle(file.Fd()), sid)
	return data, matches, err
}

type renameInformation struct {
	Replace uint32
	Root    windows.Handle
	Length  uint32
	Name    [1]uint16
}

func replaceKeys(ctx context.Context, parent windows.Handle, data []byte, sd *windows.SECURITY_DESCRIPTOR) error {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	file, err := securefile.OpenRelative(parent, ".wininit-"+hex.EncodeToString(random)+".tmp", windows.GENERIC_WRITE|windows.DELETE, windows.FILE_CREATE, windows.FILE_NON_DIRECTORY_FILE, sd)
	if err != nil {
		return fmt.Errorf("create protected authorized_keys replacement: %w", err)
	}
	defer file.Close()
	committed := false
	defer func() {
		if !committed {
			var iosb windows.IO_STATUS_BLOCK
			remove := byte(1)
			windows.NtSetInformationFile(windows.Handle(file.Fd()), &iosb, &remove, 1, windows.FileDispositionInformation)
		}
	}()
	if _, err := file.Write(data); err != nil {
		return errors.New("write authorized_keys replacement failed")
	}
	if err := file.Sync(); err != nil {
		return errors.New("flush authorized_keys replacement failed")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name, _ := windows.UTF16FromString("authorized_keys")
	length := len(name)*2 - 2
	var layout renameInformation
	buffer := make([]byte, int(unsafe.Offsetof(layout.Name))+length)
	rename := (*renameInformation)(unsafe.Pointer(&buffer[0]))
	rename.Replace, rename.Root, rename.Length = 1, parent, uint32(length)
	copy(unsafe.Slice(&rename.Name[0], length/2), name)
	var iosb windows.IO_STATUS_BLOCK
	if err := windows.NtSetInformationFile(windows.Handle(file.Fd()), &iosb, &buffer[0], uint32(len(buffer)), windows.FileRenameInformation); err != nil {
		return fmt.Errorf("atomically publish authorized_keys: %w", err)
	}
	committed = true
	return nil
}
