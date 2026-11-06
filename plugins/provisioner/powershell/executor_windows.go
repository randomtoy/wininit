//go:build windows

package powershell

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/randomtoy/wininit/internal/platform/securefile"
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
	"golang.org/x/sys/windows"
)

type NativeExecutor struct{}

func (NativeExecutor) Run(parent context.Context, script []byte, limit uint32) (*userdatav1.Execution, error) {
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if limit < 1024 || limit > userdatav1.MaxOutputBytes {
		return nil, userdatav1.ErrInvalidData
	}
	// Refuse accidental direct execution outside the reviewed host job/role.
	var job windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	requiredLimits := uint32(windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY | windows.JOB_OBJECT_LIMIT_JOB_MEMORY)
	if err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&job)), uint32(unsafe.Sizeof(job)), nil); err != nil || job.BasicLimitInformation.ActiveProcessLimit != 8 || job.JobMemoryLimit != 512<<20 || job.ProcessMemoryLimit != 512<<20 || job.BasicLimitInformation.LimitFlags&requiredLimits != requiredLimits {
		return nil, userdatav1.ErrUnavailableRuntime
	}
	var size uint32
	privileges := make([]byte, 4096)
	if err := windows.GetTokenInformation(windows.GetCurrentProcessToken(), windows.TokenPrivileges, &privileges[0], uint32(len(privileges)), &size); err != nil {
		return nil, userdatav1.ErrUnavailableRuntime
	}
	for _, name := range []string{"SeShutdownPrivilege", "SeRemoteShutdownPrivilege"} {
		encoded, _ := windows.UTF16PtrFromString(name)
		var luid windows.LUID
		if err := windows.LookupPrivilegeValue(nil, encoded, &luid); err != nil {
			return nil, userdatav1.ErrUnavailableRuntime
		}
		for _, entry := range (*windows.Tokenprivileges)(unsafe.Pointer(&privileges[0])).AllPrivileges() {
			if entry.Luid == luid {
				return nil, userdatav1.ErrUnavailableRuntime
			}
		}
	}
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return nil, userdatav1.ErrUnavailableRuntime
	}
	win, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return nil, userdatav1.ErrUnavailableRuntime
	}
	image, err := securefile.TrustedSystemPowerShell()
	if err != nil {
		return nil, userdatav1.ErrUnavailableRuntime
	}
	defer image.Close()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	output := &outputCounter{limit: uint64(limit), cancel: cancel}
	path := filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.CommandContext(ctx, path, bootstrapArguments()...)
	cmd.Dir = system
	// Windows PowerShell 5.1 uses PATHEXT to recognize native executables.
	// Without it, explicit .exe invocations can use asynchronous file association
	// launch and leave LASTEXITCODE at zero. Supply fixed OS defaults, never
	// inherited search paths or command processors from the host environment.
	cmd.Env = []string{"SystemRoot=" + win, "WINDIR=" + win, "SystemDrive=" + filepath.VolumeName(win), "PATH=" + system + ";" + win + ";" + filepath.Dir(path), "PATHEXT=.COM;.EXE;.BAT;.CMD", "ComSpec=" + filepath.Join(system, "cmd.exe"), "PSModulePath=" + filepath.Join(filepath.Dir(path), "Modules")}
	for _, folder := range []struct {
		name string
		id   *windows.KNOWNFOLDERID
	}{{"ProgramData", windows.FOLDERID_ProgramData}, {"USERPROFILE", windows.FOLDERID_Profile}} {
		value, err := windows.KnownFolderPath(folder.id, 0)
		if err == nil {
			cmd.Env = append(cmd.Env, folder.name+"="+value)
		}
	}
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout, cmd.Stderr = output, output
	cmd.SysProcAttr = &windows.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	cmd.WaitDelay = time.Second
	err = cmd.Run()
	if parent.Err() != nil {
		return nil, parent.Err()
	}
	result := &userdatav1.Execution{OutputBytes: output.Bytes()}
	if output.exceeded.Load() {
		result.Outcome = userdatav1.Outcome_OUTPUT_LIMIT
		if cmd.ProcessState != nil {
			result.ExitCode = uint32(cmd.ProcessState.ExitCode())
		}
		return result, nil
	}
	if cmd.ProcessState == nil {
		return nil, userdatav1.ErrUnavailableRuntime
	}
	result.ExitCode = uint32(cmd.ProcessState.ExitCode())
	switch {
	case result.ExitCode == 0 && err == nil:
		result.Outcome = userdatav1.Outcome_SUCCESS
	case result.ExitCode == 3010:
		result.Outcome = userdatav1.Outcome_REBOOT_REQUIRED
	default:
		// An inherited output pipe kept open by a descendant is a failure too.
		// The host closes the outer job after the response, killing descendants.
		if result.ExitCode == 0 || errors.Is(err, exec.ErrWaitDelay) {
			result.ExitCode = 1
		}
		result.Outcome = userdatav1.Outcome_EXIT_FAILURE
	}
	return result, nil
}
