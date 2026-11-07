//go:build windows

package pluginprocess

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"os/exec"
	"unsafe"
)

func prepareProcessProfile(ctx context.Context, cmd *exec.Cmd, role processRole) (*processControl, error) {
	if role == roleDatasource {
		return prepareProcess(ctx, cmd)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var original windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &original); err != nil {
		return nil, err
	}
	defer original.Close()
	user, err := original.GetTokenUser()
	if err != nil {
		return nil, err
	}
	if !original.IsElevated() && !user.User.Sid.IsWellKnown(windows.WinLocalSystemSid) {
		return nil, errors.New("privileged provisioner requires an elevated agent account")
	}
	// This role intentionally retains provisioning access. Deleting direct
	// reboot privileges is defense in depth, not a sandbox for hostile SYSTEM
	// scripts: only explicitly hash-approved trusted scripts are supported.
	var deleted []windows.LUIDAndAttributes
	for _, name := range []string{"SeShutdownPrivilege", "SeRemoteShutdownPrivilege"} {
		encoded, _ := windows.UTF16PtrFromString(name)
		var luid windows.LUID
		if err := windows.LookupPrivilegeValue(nil, encoded, &luid); err != nil {
			return nil, err
		}
		deleted = append(deleted, windows.LUIDAndAttributes{Luid: luid})
	}
	control := &processControl{ctx: ctx}
	result, _, callErr := createRestrictedToken.Call(uintptr(original), 0, 0, 0, uintptr(len(deleted)), uintptr(unsafe.Pointer(&deleted[0])), 0, 0, uintptr(unsafe.Pointer(&control.token)))
	if result == 0 {
		return nil, callErr
	}
	fail := func(err error) (*processControl, error) { control.close(); return nil, err }
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;;GA;;;SY)(A;;GA;;;BA)")
	if err != nil {
		return fail(err)
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	control.job, err = windows.CreateJobObject(&attributes, nil)
	if err != nil {
		return fail(err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY | windows.JOB_OBJECT_LIMIT_JOB_MEMORY
	limits.BasicLimitInformation.ActiveProcessLimit = 8
	limits.ProcessMemoryLimit = 512 << 20
	limits.JobMemoryLimit = 512 << 20
	if role == roleProvisioner {
		limits.BasicLimitInformation.ActiveProcessLimit = 1
		limits.ProcessMemoryLimit = 256 << 20
		limits.JobMemoryLimit = 256 << 20
	}
	if _, err := windows.SetInformationJobObject(control.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fail(err)
	}
	control.desktop, err = newPrivateDesktopWithSecurity("O:BAG:BAD:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user.User.Sid.String() + ")S:(ML;;NW;;;HI)")
	if err != nil {
		return fail(err)
	}
	cmd.Dir, err = windows.GetSystemWindowsDirectory()
	if err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	return control, nil
}
