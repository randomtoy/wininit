//go:build windows

package pluginprocess

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"github.com/randomtoy/wininit/internal/platform/securefile"
	"golang.org/x/sys/windows"
)

var createRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

type processControl struct {
	token windows.Token
	job   windows.Handle
	once  sync.Once
	mu    sync.Mutex
}

func prepareProcess(cmd *exec.Cmd) (*processControl, error) {
	var original windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY, &original); err != nil {
		return nil, err
	}
	defer original.Close()
	user, err := original.GetTokenUser()
	if err != nil {
		return nil, err
	}
	admin, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	disabled := []windows.SIDAndAttributes{{Sid: user.User.Sid}, {Sid: admin}}
	control := &processControl{}
	result, _, callErr := createRestrictedToken.Call(uintptr(original), 1, uintptr(len(disabled)), uintptr(unsafe.Pointer(&disabled[0])), 0, 0, 0, 0, uintptr(unsafe.Pointer(&control.token)))
	if result == 0 {
		return nil, callErr
	}
	fail := func(err error) (*processControl, error) { control.close(); return nil, err }
	low, err := windows.StringToSid("S-1-16-4096")
	if err != nil {
		return fail(err)
	}
	label := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: low, Attributes: windows.SE_GROUP_INTEGRITY}}
	if err := windows.SetTokenInformation(control.token, windows.TokenIntegrityLevel, (*byte)(unsafe.Pointer(&label)), label.Size()); err != nil {
		return fail(err)
	}
	control.job, err = windows.CreateJobObject(nil, nil)
	if err != nil {
		return fail(err)
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_ACTIVE_PROCESS | windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
	limits.BasicLimitInformation.ActiveProcessLimit = 1
	limits.ProcessMemoryLimit = 256 << 20
	if _, err := windows.SetInformationJobObject(control.job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return fail(err)
	}
	// UI limits prohibit nested jobs (including ordinary CI/service hosts).
	// Use a windowless low-integrity process and keep job containment mandatory.
	cmd.SysProcAttr = &syscall.SysProcAttr{Token: syscall.Token(control.token), HideWindow: true, CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW}
	windowsDir, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return fail(err)
	}
	cmd.Dir = windowsDir
	return control, nil
}

func (c *processControl) started(cmd *exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(c.job, process); err != nil {
		return err
	}
	// os/exec closes the initial thread handle. The process is still suspended
	// and has exactly one thread, so enumerate that thread before resuming it.
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	for err := windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != uint32(cmd.Process.Pid) {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		defer windows.CloseHandle(thread)
		if _, err := windows.ResumeThread(thread); err != nil {
			return err
		}
		return nil
	}
	return errors.New("suspended plugin thread was not found")
}
func (c *processControl) kill(cmd *exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	// Process.Kill detects a reaped process. Job termination additionally covers
	// every job member; the active-process limit forbids child-process creation.
	err := cmd.Process.Kill()
	if c.job != 0 {
		windows.TerminateJobObject(c.job, 1)
	}
	return err
}
func (c *processControl) close() {
	c.once.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.job != 0 {
			windows.CloseHandle(c.job)
			c.job = 0
		}
		if c.token != 0 {
			c.token.Close()
			c.token = 0
		}
	})
}
func openExecutable(path string) (*os.File, error) { return securefile.TrustedFile(path, false) }
func environment() []string {
	path, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		return nil
	}
	return []string{"SystemRoot=" + path, "WINDIR=" + path}
}
