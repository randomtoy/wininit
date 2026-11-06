//go:build windows

// Package securefile opens local files without reparse traversal and checks ACLs.
package securefile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/randomtoy/wininit/internal/identity"
	"golang.org/x/sys/windows"
)

// OpenRelative uses an already-open directory handle. No component may reparse.
// Omitting FILE_SHARE_DELETE pins directories against rename while in use.
func OpenRelative(parent windows.Handle, name string, access, disposition, options uint32, sd *windows.SECURITY_DESCRIPTOR) (*os.File, error) {
	if parent != 0 && (!ordinaryComponent(name) || strings.ContainsAny(name, "\\/:\x00")) {
		return nil, errors.New("expected one unambiguous filesystem component")
	}
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := windows.OBJECT_ATTRIBUTES{RootDirectory: parent, ObjectName: objectName, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE, SecurityDescriptor: sd}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var handle windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	share := uint32(windows.FILE_SHARE_READ)
	if options&windows.FILE_DIRECTORY_FILE != 0 {
		share |= windows.FILE_SHARE_WRITE
	}
	if access&windows.DELETE != 0 {
		share |= windows.FILE_SHARE_DELETE
	}
	// Every handle is inspected before use. FILE_READ_ATTRIBUTES is required
	// even when the caller only requested directory traversal or file writes.
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE|windows.FILE_READ_ATTRIBUTES, &oa, &iosb, nil, windows.FILE_ATTRIBUTE_NORMAL,
		share, disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		file.Close()
		return nil, fmt.Errorf("inspect opened file attributes: %w", err)
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		file.Close()
		return nil, errors.New("reparse points are not permitted")
	}
	return file, nil
}

func OpenLocal(path string, access, options uint32) (*os.File, error) {
	if err := checkLocalPath(path); err != nil {
		return nil, err
	}
	volume, err := windows.UTF16PtrFromString(filepath.VolumeName(path) + `\`)
	if err != nil {
		return nil, err
	}
	driveType := windows.GetDriveType(volume)
	if driveType != windows.DRIVE_FIXED && driveType != windows.DRIVE_REMOVABLE && driveType != windows.DRIVE_CDROM {
		return nil, errors.New("remote or unknown drives are not permitted")
	}
	return OpenRelative(0, `\??\`+filepath.Clean(path), access, windows.FILE_OPEN, options, nil)
}

// The verifier uses NT handles while process creation uses Win32 paths. Reject
// stream/device/trailing-dot aliases so they cannot name different objects.
func checkLocalPath(path string) error {
	if !identity.LocalAbsolutePath(path) || len(filepath.VolumeName(path)) != 2 || strings.Contains(path[2:], ":") {
		return errors.New("expected a local drive-letter path without streams")
	}
	for _, component := range strings.FieldsFunc(path[2:], func(ch rune) bool { return ch == '\\' || ch == '/' }) {
		if !ordinaryComponent(component) {
			return errors.New("ambiguous Win32 path component is not permitted")
		}
	}
	return nil
}

func ordinaryComponent(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	base, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch base {
	case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
		return false
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '0' && base[3] <= '9' {
		return false
	}
	return true
}

// TrustedACL requires an administrator/SYSTEM owner and a non-null DACL.
// Executables may be readable by users; secrets may not be readable by others.
func TrustedACL(file *os.File, secret bool) error {
	return trustedACL(file, secret, false)
}
func trustedACL(file *os.File, secret, system bool) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return errors.New("cannot inspect protected file security")
	}
	owner, _, err := sd.Owner()
	if err != nil || !trustedFor(owner, system) {
		return errors.New("protected file must be owned by SYSTEM or Administrators")
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return errors.New("protected file requires an explicit access-control list")
	}
	// FILE_DELETE_CHILD on a directory can delete a protected child even when
	// that child's own DACL does not grant DELETE.
	const fileDeleteChild = 0x0040
	dangerous := uint32(windows.GENERIC_WRITE | windows.GENERIC_ALL | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | fileDeleteChild | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER)
	if secret {
		dangerous |= windows.GENERIC_READ | windows.GENERIC_EXECUTE | windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE
	}
	for index := uint16(0); index < dacl.AceCount; index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, uint32(index), &ace); err != nil {
			return errors.New("cannot inspect protected file access")
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return errors.New("unsupported protected file access entry")
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if uint32(ace.Mask)&dangerous != 0 && !trustedFor(sid, system) {
			return errors.New("protected file permits access by an untrusted principal")
		}
	}
	return nil
}

func trusted(sid *windows.SID) bool {
	return sid != nil && (sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}
func trustedFor(sid *windows.SID, system bool) bool {
	// Windows servicing owns System32 images/directories and may hard-link
	// them into WinSxS. This exception is never used for configured plugins.
	return trusted(sid) || system && sid != nil && sid.String() == "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
}

func TrustedFile(path string, secret bool) (*os.File, error) {
	if err := checkLocalPath(path); err != nil {
		return nil, err
	}
	parent, err := OpenLocal(filepath.Dir(path), windows.READ_CONTROL|windows.FILE_LIST_DIRECTORY, windows.FILE_DIRECTORY_FILE)
	if err != nil {
		return nil, fmt.Errorf("open protected parent directory: %w", err)
	}
	defer parent.Close()
	return trustedFileAt(parent, filepath.Base(path), secret)
}

func trustedFileAt(parent *os.File, name string, secret bool) (*os.File, error) {
	return trustedFileAtMode(parent, name, secret, false)
}
func trustedFileAtMode(parent *os.File, name string, secret, system bool) (*os.File, error) {
	if err := trustedACL(parent, false, system); err != nil {
		return nil, err
	}
	file, err := OpenRelative(windows.Handle(parent.Fd()), name, windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		return nil, fmt.Errorf("open protected file: %w", err)
	}
	if err := trustedACL(file, secret, system); err != nil {
		file.Close()
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil || info.NumberOfLinks == 0 || !system && info.NumberOfLinks != 1 {
		file.Close()
		return nil, errors.New("protected files must have exactly one hard link")
	}
	return file, nil
}

// PinnedFile keeps every ancestor fixed while a verified image is launched by
// its Win32 path. A locked file alone cannot protect against directory swaps.
type PinnedFile struct {
	*os.File
	parents []*os.File
	once    sync.Once
	err     error
}

func (file *PinnedFile) Close() error {
	file.once.Do(func() {
		file.err = file.File.Close()
		for index := len(file.parents) - 1; index >= 0; index-- {
			file.err = errors.Join(file.err, file.parents[index].Close())
		}
	})
	return file.err
}

func TrustedExecutable(path string) (_ *PinnedFile, err error) {
	return pinnedExecutable(path, false)
}

// TrustedSystemPowerShell opens only the native OS PowerShell image, allowing
// Windows servicing ownership/hard links while preserving path and ACL pins.
func TrustedSystemPowerShell() (*PinnedFile, error) {
	directory, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, err
	}
	return pinnedExecutable(filepath.Join(directory, "WindowsPowerShell", "v1.0", "powershell.exe"), true)
}

func pinnedExecutable(path string, system bool) (_ *PinnedFile, err error) {
	if err := checkLocalPath(path); err != nil {
		return nil, err
	}
	volume := filepath.VolumeName(path)
	parts := strings.Split(strings.TrimPrefix(filepath.Clean(path), volume+`\`), `\`)
	if len(parts) == 0 || parts[0] == "" {
		return nil, errors.New("executable must name a file below a drive root")
	}
	parent, err := OpenLocal(volume+`\`, windows.READ_CONTROL|windows.FILE_TRAVERSE, windows.FILE_DIRECTORY_FILE)
	if err != nil {
		return nil, err
	}
	parents := []*os.File{parent}
	defer func() {
		if err != nil {
			for index := len(parents) - 1; index >= 0; index-- {
				parents[index].Close()
			}
		}
	}()
	for _, component := range parts[:len(parts)-1] {
		parent, err = OpenRelative(windows.Handle(parent.Fd()), component, windows.READ_CONTROL|windows.FILE_TRAVERSE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, nil)
		if err != nil {
			return nil, err
		}
		parents = append(parents, parent)
	}
	image, err := trustedFileAtMode(parent, parts[len(parts)-1], false, system)
	if err != nil {
		return nil, err
	}
	return &PinnedFile{File: image, parents: parents}, nil
}
