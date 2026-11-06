//go:build windows

// Package securefile opens local files without reparse traversal and checks ACLs.
package securefile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/randomtoy/wininit/internal/identity"
	"golang.org/x/sys/windows"
)

// OpenRelative uses an already-open directory handle. No component may reparse.
// Omitting FILE_SHARE_DELETE pins directories against rename while in use.
func OpenRelative(parent windows.Handle, name string, access, disposition, options uint32, sd *windows.SECURITY_DESCRIPTOR) (*os.File, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return nil, err
	}
	oa := windows.OBJECT_ATTRIBUTES{RootDirectory: parent, ObjectName: objectName, Attributes: windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE, SecurityDescriptor: sd}
	oa.Length = uint32(unsafe.Sizeof(oa))
	var handle windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	err = windows.NtCreateFile(&handle, access|windows.SYNCHRONIZE, &oa, &iosb, nil, windows.FILE_ATTRIBUTE_NORMAL,
		windows.FILE_SHARE_READ, disposition, options|windows.FILE_SYNCHRONOUS_IO_NONALERT|windows.FILE_OPEN_REPARSE_POINT, 0, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(handle), name)
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		file.Close()
		return nil, errors.New("reparse points are not permitted")
	}
	return file, nil
}

func OpenLocal(path string, access, options uint32) (*os.File, error) {
	if !identity.LocalAbsolutePath(path) || len(filepath.VolumeName(path)) != 2 || strings.Contains(path[2:], ":") {
		return nil, errors.New("expected a local drive-letter path")
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

// TrustedACL requires an administrator/SYSTEM owner and a non-null DACL.
// Executables may be readable by users; secrets may not be readable by others.
func TrustedACL(file *os.File, secret bool) error {
	sd, err := windows.GetSecurityInfo(windows.Handle(file.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return errors.New("cannot inspect protected file security")
	}
	owner, _, err := sd.Owner()
	if err != nil || !trusted(owner) {
		return errors.New("protected file must be owned by SYSTEM or Administrators")
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return errors.New("protected file requires an explicit access-control list")
	}
	dangerous := uint32(windows.GENERIC_WRITE | windows.GENERIC_ALL | windows.FILE_WRITE_DATA | windows.FILE_APPEND_DATA | windows.FILE_WRITE_EA | windows.FILE_WRITE_ATTRIBUTES | windows.DELETE | windows.WRITE_DAC | windows.WRITE_OWNER)
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
		if uint32(ace.Mask)&dangerous != 0 && !trusted(sid) {
			return errors.New("protected file permits access by an untrusted principal")
		}
	}
	return nil
}

func trusted(sid *windows.SID) bool {
	return sid != nil && (sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid))
}

func TrustedFile(path string, secret bool) (*os.File, error) {
	parent, err := OpenLocal(filepath.Dir(path), windows.READ_CONTROL|windows.FILE_LIST_DIRECTORY, windows.FILE_DIRECTORY_FILE)
	if err != nil {
		return nil, errors.New("open protected parent directory failed")
	}
	defer parent.Close()
	if err := TrustedACL(parent, false); err != nil {
		return nil, err
	}
	file, err := OpenRelative(windows.Handle(parent.Fd()), filepath.Base(path), windows.GENERIC_READ|windows.READ_CONTROL, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if err != nil {
		return nil, errors.New("open protected file failed")
	}
	if err := TrustedACL(file, secret); err != nil {
		file.Close()
		return nil, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil || info.NumberOfLinks != 1 {
		file.Close()
		return nil, errors.New("protected files must have exactly one hard link")
	}
	return file, nil
}
