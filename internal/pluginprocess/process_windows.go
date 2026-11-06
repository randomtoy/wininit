//go:build windows

package pluginprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/randomtoy/wininit/internal/platform/securefile"
	"golang.org/x/sys/windows"
)

var createRestrictedToken = windows.NewLazySystemDLL("advapi32.dll").NewProc("CreateRestrictedToken")

type processControl struct {
	ctx                  context.Context
	token                windows.Token
	job, process, thread windows.Handle
	desktop              *privateDesktop
	stopCancellation     func() bool
	assigned             bool
	once                 sync.Once
	mu                   sync.Mutex
}

func prepareProcess(ctx context.Context, cmd *exec.Cmd) (*processControl, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var original windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY|windows.TOKEN_DUPLICATE|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &original); err != nil {
		return nil, err
	}
	defer original.Close()
	admin, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, err
	}
	disabled := []windows.SIDAndAttributes{{Sid: admin}}
	var restricted []windows.SIDAndAttributes
	for _, kind := range []windows.WELL_KNOWN_SID_TYPE{windows.WinBuiltinUsersSid, windows.WinAuthenticatedUserSid, windows.WinWorldSid} {
		sid, err := windows.CreateWellKnownSid(kind)
		if err != nil {
			return nil, err
		}
		restricted = append(restricted, windows.SIDAndAttributes{Sid: sid})
	}
	control := &processControl{ctx: ctx}
	// A second ACL pass as ordinary principals prevents a SYSTEM caller from
	// using its retained user SID to read SYSTEM/Admin-only host secrets.
	result, _, callErr := createRestrictedToken.Call(uintptr(original), 1, uintptr(len(disabled)), uintptr(unsafe.Pointer(&disabled[0])), 0, 0, uintptr(len(restricted)), uintptr(unsafe.Pointer(&restricted[0])), uintptr(unsafe.Pointer(&control.token)))
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
	// A child may query limits, but cannot alter, assign, or terminate jobs.
	jobSD, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;0x4;;;AU)")
	if err != nil {
		return fail(err)
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: jobSD}
	control.job, err = windows.CreateJobObject(&attributes, nil)
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
	// UI job limits prohibit nesting. Use separate noninteractive UI objects.
	control.desktop, err = newPrivateDesktop()
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

func (c *processControl) start(cmd *exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	stdout, ok := cmd.Stdout.(*os.File)
	if !ok {
		return errors.New("plugin stdout pipe is not configured")
	}
	stderr, ok := cmd.Stderr.(*os.File)
	if !ok {
		return errors.New("plugin stderr pipe is not configured")
	}
	defer stdout.Close()
	defer stderr.Close()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	input, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer input.Close()
	// Inherit exactly three handles. Images, tokens, jobs, secrets, and UI
	// objects remain private to the parent.
	handles := make([]windows.Handle, 0, 3)
	defer func() {
		for _, handle := range handles {
			windows.CloseHandle(handle)
		}
	}()
	for _, file := range []*os.File{input, stdout, stderr} {
		var handle windows.Handle
		if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(file.Fd()), windows.CurrentProcess(), &handle, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
			return err
		}
		handles = append(handles, handle)
	}
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return err
	}
	defer attributes.Delete()
	if err := attributes.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return err
	}
	application, err := windows.UTF16PtrFromString(cmd.Path)
	if err != nil {
		return err
	}
	commandLine, err := windows.UTF16PtrFromString(windows.EscapeArg(cmd.Path))
	if err != nil {
		return err
	}
	directory, err := windows.UTF16PtrFromString(cmd.Dir)
	if err != nil {
		return err
	}
	desktop, err := windows.UTF16PtrFromString(c.desktop.name)
	if err != nil {
		return err
	}
	entries := append([]string(nil), cmd.Env...)
	sort.Slice(entries, func(i, j int) bool {
		left, _, _ := strings.Cut(entries[i], "=")
		right, _, _ := strings.Cut(entries[j], "=")
		return strings.ToUpper(left) < strings.ToUpper(right)
	})
	var environment []uint16
	for _, entry := range entries {
		value, err := windows.UTF16FromString(entry)
		if err != nil {
			return err
		}
		environment = append(environment, value...)
	}
	environment = append(environment, 0)
	if len(environment) == 1 {
		environment = append(environment, 0)
	}
	startup := windows.StartupInfoEx{}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Desktop = desktop
	startup.Flags = windows.STARTF_USESTDHANDLES | windows.STARTF_USESHOWWINDOW
	startup.ShowWindow = windows.SW_HIDE
	startup.StdInput, startup.StdOutput, startup.StdErr = handles[0], handles[1], handles[2]
	startup.ProcThreadAttributeList = attributes.List()
	flags := uint32(windows.CREATE_SUSPENDED | windows.CREATE_NO_WINDOW | windows.CREATE_UNICODE_ENVIRONMENT | windows.EXTENDED_STARTUPINFO_PRESENT)
	var process windows.ProcessInformation
	if err := windows.CreateProcessAsUser(c.token, application, commandLine, nil, nil, true, flags, &environment[0], directory, &startup.StartupInfo, &process); err != nil {
		return err
	}
	c.process, c.thread = process.Process, process.Thread
	cmd.Process, err = os.FindProcess(int(process.ProcessId))
	if err != nil {
		windows.TerminateProcess(c.process, 1)
		windows.WaitForSingleObject(c.process, windows.INFINITE)
		return err
	}
	c.stopCancellation = context.AfterFunc(c.ctx, func() { _ = cmd.Cancel() })
	return nil
}

func (c *processControl) started(*exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := windows.AssignProcessToJobObject(c.job, c.process); err != nil {
		return err
	}
	c.assigned = true
	_, err := windows.ResumeThread(c.thread)
	windows.CloseHandle(c.thread)
	c.thread = 0
	return err
}
func (c *processControl) wait(cmd *exec.Cmd) (uint32, error) {
	c.mu.Lock()
	handle := c.process
	c.mu.Unlock()
	if handle == 0 {
		return 0, os.ErrProcessDone
	}
	_, err := windows.WaitForSingleObject(handle, windows.INFINITE)
	if cmd.Process != nil {
		defer cmd.Process.Release()
	}
	if err != nil {
		return 0, err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(handle, &code); err != nil {
		return 0, err
	}
	if code != 0 {
		return code, fmt.Errorf("plugin exited (code 0x%x)", code)
	}
	return code, nil
}
func (c *processControl) kill(*exec.Cmd) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.process == 0 {
		return os.ErrProcessDone
	}
	var code uint32
	if err := windows.GetExitCodeProcess(c.process, &code); err == nil && code != 259 {
		return os.ErrProcessDone
	}
	if c.job != 0 && c.assigned {
		return windows.TerminateJobObject(c.job, 1)
	}
	return windows.TerminateProcess(c.process, 1)
}
func (c *processControl) close() {
	c.once.Do(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.stopCancellation != nil {
			c.stopCancellation()
			c.stopCancellation = nil
		}
		if c.job != 0 {
			windows.CloseHandle(c.job)
			c.job = 0
		}
		if c.thread != 0 {
			windows.CloseHandle(c.thread)
			c.thread = 0
		}
		if c.process != 0 {
			windows.CloseHandle(c.process)
			c.process = 0
		}
		if c.token != 0 {
			c.token.Close()
			c.token = 0
		}
		if c.desktop != nil {
			c.desktop.close()
			c.desktop = nil
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
