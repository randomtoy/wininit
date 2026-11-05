//go:build windows

package state

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	privateNamespaceAlias = "WininitPrivate"
	boundaryName          = "WininitAdministratorsBoundary"
	machineMutexName      = privateNamespaceAlias + `\Provisioning`
	protectedObjectSDDL   = "D:P(A;;GA;;;SY)(A;;GA;;;BA)"
)

var (
	kernel32                       = windows.NewLazySystemDLL("kernel32.dll")
	procAddSIDToBoundaryDescriptor = kernel32.NewProc("AddSIDToBoundaryDescriptor")
	procCreateBoundaryDescriptor   = kernel32.NewProc("CreateBoundaryDescriptorW")
	procCreatePrivateNamespace     = kernel32.NewProc("CreatePrivateNamespaceW")
	procDeleteBoundaryDescriptor   = kernel32.NewProc("DeleteBoundaryDescriptor")
	procOpenPrivateNamespace       = kernel32.NewProc("OpenPrivateNamespaceW")

	namespaceOnce   sync.Once
	namespaceHandle windows.Handle
	namespaceErr    error
)

type machineLockResult struct {
	release func()
	err     error
}

// AcquireMachineLock excludes concurrent service and console provisioning.
// A private namespace prevents callers outside the Administrators boundary
// from naming the mutex. A dedicated locked OS thread owns and releases the
// mutex because Windows mutex ownership is thread-affine.
func AcquireMachineLock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := ensurePrivateNamespace(); err != nil {
		return nil, err
	}

	result := make(chan machineLockResult, 1)
	go ownMachineLock(ctx, machineMutexName, result)
	acquired := <-result
	return acquired.release, acquired.err
}

func ensurePrivateNamespace() error {
	namespaceOnce.Do(func() {
		namespaceHandle, namespaceErr = createOrOpenPrivateNamespace(privateNamespaceAlias, boundaryName)
	})
	return namespaceErr
}

func createOrOpenPrivateNamespace(aliasName, boundaryDescriptorName string) (windows.Handle, error) {
	boundaryNamePointer, err := windows.UTF16PtrFromString(boundaryDescriptorName)
	if err != nil {
		return 0, err
	}
	boundary, _, callErr := procCreateBoundaryDescriptor.Call(uintptr(unsafe.Pointer(boundaryNamePointer)), 0)
	if boundary == 0 {
		return 0, fmt.Errorf("create lifecycle boundary descriptor: %w", nonzeroCallError(callErr))
	}
	defer func() { procDeleteBoundaryDescriptor.Call(boundary) }()

	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return 0, fmt.Errorf("create Administrators boundary SID: %w", err)
	}
	result, _, callErr := procAddSIDToBoundaryDescriptor.Call(
		uintptr(unsafe.Pointer(&boundary)),
		uintptr(unsafe.Pointer(administrators)),
	)
	if result == 0 {
		return 0, fmt.Errorf("add Administrators SID to lifecycle boundary: %w", nonzeroCallError(callErr))
	}

	attributes, descriptor, err := protectedSecurityAttributes()
	if err != nil {
		return 0, err
	}
	alias, err := windows.UTF16PtrFromString(aliasName)
	if err != nil {
		return 0, err
	}
	handle, _, createErr := procCreatePrivateNamespace.Call(
		uintptr(unsafe.Pointer(attributes)),
		boundary,
		uintptr(unsafe.Pointer(alias)),
	)
	runtime.KeepAlive(descriptor)
	runtime.KeepAlive(administrators)
	if handle != 0 {
		return windows.Handle(handle), nil
	}
	if !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		return 0, fmt.Errorf("create lifecycle private namespace: %w", nonzeroCallError(createErr))
	}

	handle, _, openErr := procOpenPrivateNamespace.Call(boundary, uintptr(unsafe.Pointer(alias)))
	if handle == 0 {
		return 0, fmt.Errorf("open lifecycle private namespace: %w", nonzeroCallError(openErr))
	}
	return windows.Handle(handle), nil
}

func ownMachineLock(ctx context.Context, mutexName string, result chan<- machineLockResult) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	attributes, descriptor, err := protectedSecurityAttributes()
	if err != nil {
		result <- machineLockResult{err: err}
		return
	}
	name, err := windows.UTF16PtrFromString(mutexName)
	if err != nil {
		result <- machineLockResult{err: err}
		return
	}
	handle, createErr := windows.CreateMutex(attributes, false, name)
	runtime.KeepAlive(descriptor)
	if createErr != nil && !errors.Is(createErr, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		result <- machineLockResult{err: fmt.Errorf("create lifecycle mutex: %w", createErr)}
		return
	}
	if handle == 0 {
		result <- machineLockResult{err: errors.New("create lifecycle mutex returned an invalid handle")}
		return
	}
	defer func() {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
	}()

	waitResult, err := windows.WaitForSingleObject(handle, 0)
	if err != nil {
		result <- machineLockResult{err: fmt.Errorf("wait for lifecycle mutex: %w", err)}
		return
	}
	switch waitResult {
	case windows.WAIT_OBJECT_0, windows.WAIT_ABANDONED:
	case uint32(windows.WAIT_TIMEOUT):
		result <- machineLockResult{err: errors.New("another wininit lifecycle process is running")}
		return
	default:
		result <- machineLockResult{err: fmt.Errorf("wait for lifecycle mutex returned unexpected status %#x", waitResult)}
		return
	}
	if err := ctx.Err(); err != nil {
		_ = windows.ReleaseMutex(handle)
		result <- machineLockResult{err: err}
		return
	}

	releaseRequest := make(chan struct{})
	released := make(chan struct{})
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() { close(releaseRequest) })
		<-released
	}
	result <- machineLockResult{release: release}
	<-releaseRequest
	_ = windows.ReleaseMutex(handle)
	_ = windows.CloseHandle(handle)
	handle = 0
	close(released)
}

func protectedSecurityAttributes() (*windows.SecurityAttributes, *windows.SECURITY_DESCRIPTOR, error) {
	descriptor, err := windows.SecurityDescriptorFromString(protectedObjectSDDL)
	if err != nil {
		return nil, nil, fmt.Errorf("create protected lifecycle security descriptor: %w", err)
	}
	attributes := &windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: descriptor,
	}
	return attributes, descriptor, nil
}

func nonzeroCallError(err error) error {
	if err == nil || errors.Is(err, windows.ERROR_SUCCESS) {
		return errors.New("Windows API failed without an error code")
	}
	return err
}
