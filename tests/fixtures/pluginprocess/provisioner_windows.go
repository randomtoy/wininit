//go:build windows

package main

import (
	"errors"
	"golang.org/x/sys/windows"
	"os/exec"
	"unsafe"
)

func checkProvisionerRestrictions() error {
	var limits windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	required := uint32(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY | windows.JOB_OBJECT_LIMIT_JOB_MEMORY)
	if err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits)), nil); err != nil {
		return err
	}
	if limits.BasicLimitInformation.ActiveProcessLimit != 1 || limits.ProcessMemoryLimit != 256<<20 || limits.JobMemoryLimit != 256<<20 || limits.BasicLimitInformation.LimitFlags&required != required {
		return errors.New("incorrect provisioner job")
	}
	token := windows.GetCurrentProcessToken()
	if !token.IsElevated() {
		return errors.New("provisioner lost elevation")
	}
	var size uint32
	buffer := make([]byte, 4096)
	if err := windows.GetTokenInformation(token, windows.TokenPrivileges, &buffer[0], uint32(len(buffer)), &size); err != nil {
		return err
	}
	for _, name := range []string{"SeShutdownPrivilege", "SeRemoteShutdownPrivilege"} {
		var luid windows.LUID
		encoded, _ := windows.UTF16PtrFromString(name)
		if err := windows.LookupPrivilegeValue(nil, encoded, &luid); err != nil {
			return err
		}
		for _, entry := range (*windows.Tokenprivileges)(unsafe.Pointer(&buffer[0])).AllPrivileges() {
			if entry.Luid == luid {
				return errors.New("shutdown privilege retained")
			}
		}
	}
	// The configured job permits this plugin alone, even with the elevated token.
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	child := exec.Command(system+`\cmd.exe`, "/d", "/c", "exit", "0")
	if err := child.Run(); err == nil {
		return errors.New("declarative provisioner spawned a child")
	}
	return nil
}
