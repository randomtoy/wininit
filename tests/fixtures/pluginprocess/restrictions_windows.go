//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"path/filepath"
	"unsafe"
)

func checkRestrictions(directory string) error {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil || user.User.Attributes&windows.SE_GROUP_USE_FOR_DENY_ONLY == 0 {
		return errors.New("user SID is not deny-only")
	}
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
