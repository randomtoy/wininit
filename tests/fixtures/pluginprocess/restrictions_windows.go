//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"
)

func checkRestrictions(directory string) error {
	station, _, callErr := windows.NewLazySystemDLL("user32.dll").NewProc("GetProcessWindowStation").Call()
	if station == 0 {
		return callErr
	}
	nameBuffer := make([]uint16, 256)
	var required uint32
	ok, _, callErr := windows.NewLazySystemDLL("user32.dll").NewProc("GetUserObjectInformationW").Call(station, 2, uintptr(unsafe.Pointer(&nameBuffer[0])), uintptr(len(nameBuffer)*2), uintptr(unsafe.Pointer(&required)))
	if ok == 0 {
		return callErr
	}
	if !strings.HasPrefix(windows.UTF16ToString(nameBuffer), "Wininit-") {
		return errors.New("child inherited a host window station")
	}
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil); err != nil {
		return err
	}
	if limits.BasicLimitInformation.ActiveProcessLimit != 1 || limits.BasicLimitInformation.LimitFlags&windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE == 0 || limits.ProcessMemoryLimit != 256<<20 {
		return errors.New("job limits are not installed")
	}
	token := windows.GetCurrentProcessToken()
	restrictedBuffer := make([]byte, 4096)
	var restrictedSize uint32
	if err := windows.GetTokenInformation(token, windows.TokenRestrictedSids, &restrictedBuffer[0], uint32(len(restrictedBuffer)), &restrictedSize); err != nil {
		return err
	}
	restricted := (*windows.Tokengroups)(unsafe.Pointer(&restrictedBuffer[0]))
	seen := map[string]bool{}
	for _, group := range restricted.AllGroups() {
		seen[group.Sid.String()] = true
	}
	if len(seen) != 4 || !seen["S-1-1-0"] || !seen["S-1-5-11"] || !seen["S-1-5-32-545"] {
		return errors.New("ordinary restricting SIDs are not installed")
	}
	privateSIDs := 0
	for sid := range seen {
		if strings.HasPrefix(sid, "S-1-5-21-") {
			privateSIDs++
		}
	}
	if privateSIDs != 1 {
		return errors.New("per-launch restricting SID is missing")
	}
	event, err := windows.CreateEvent(nil, 1, 1, nil)
	if err != nil {
		return errors.New("restricted process cannot create its own synchronization objects")
	}
	windows.CloseHandle(event)
	groups, err := token.GetTokenGroups()
	if err != nil {
		return err
	}
	for _, group := range groups.AllGroups() {
		if group.Sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) && group.Attributes&windows.SE_GROUP_USE_FOR_DENY_ONLY == 0 {
			return errors.New("administrators SID remained enabled")
		}
	}
	buffer := make([]byte, 1024)
	var size uint32
	if err := windows.GetTokenInformation(token, windows.TokenIntegrityLevel, &buffer[0], uint32(len(buffer)), &size); err != nil {
		return err
	}
	label := (*windows.Tokenmandatorylabel)(unsafe.Pointer(&buffer[0]))
	if label.Label.Sid.String() != "S-1-16-4096" {
		return errors.New("integrity level is not low")
	}
	privilegeBuffer := make([]byte, 4096)
	if err := windows.GetTokenInformation(token, windows.TokenPrivileges, &privilegeBuffer[0], uint32(len(privilegeBuffer)), &size); err != nil {
		return err
	}
	privileges := (*windows.Tokenprivileges)(unsafe.Pointer(&privilegeBuffer[0]))
	notify, _ := windows.UTF16PtrFromString("SeChangeNotifyPrivilege")
	var allowed windows.LUID
	if err := windows.LookupPrivilegeValue(nil, notify, &allowed); err != nil {
		return err
	}
	for _, privilege := range privileges.AllPrivileges() {
		if privilege.Attributes&windows.SE_PRIVILEGE_ENABLED != 0 && privilege.Luid != allowed {
			return errors.New("unexpected enabled privilege")
		}
	}
	if _, err := os.ReadFile(filepath.Join(directory, "host-secret")); err == nil {
		return errors.New("restricted child read a host secret")
	}
	if err := os.WriteFile(filepath.Join(directory, "forbidden-write"), []byte("unexpected"), 0o600); err == nil {
		return errors.New("restricted child wrote to a protected directory")
	}
	if err := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe"), "/C", "exit", "0").Run(); err == nil {
		return errors.New("job allowed child-process creation")
	}
	return nil
}
