//go:build windows

package pluginprocess

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	createWindowStation     = user32.NewProc("CreateWindowStationW")
	getProcessWindowStation = user32.NewProc("GetProcessWindowStation")
	setProcessWindowStation = user32.NewProc("SetProcessWindowStation")
	createDesktop           = user32.NewProc("CreateDesktopW")
	closeDesktop            = user32.NewProc("CloseDesktop")
	closeWindowStation      = user32.NewProc("CloseWindowStation")
	// Association is process-wide. The host is headless; this serialized scope
	// restores it before any child is created.
	desktopCreationMu sync.Mutex
)

type privateDesktop struct {
	name             string
	station, desktop windows.Handle
}

func newPrivateDesktop() (*privateDesktop, error) {
	desktopCreationMu.Lock()
	defer desktopCreationMu.Unlock()
	type created struct {
		desktop *privateDesktop
		thread  windows.Handle
		err     error
	}
	result := make(chan created, 1)
	go func() {
		// CreateDesktop attaches the calling thread. Exiting while still locked
		// makes Go dispose of it; no UI-affiliated thread returns to the pool.
		runtime.LockOSThread()
		var thread windows.Handle
		if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.CurrentThread(), windows.CurrentProcess(), &thread, windows.SYNCHRONIZE, false, 0); err != nil {
			result <- created{err: err}
			return
		}
		desktop, err := createPrivateDesktopOnThread()
		result <- created{desktop, thread, err}
	}()
	outcome := <-result
	if outcome.thread != 0 {
		_, err := windows.WaitForSingleObject(outcome.thread, windows.INFINITE)
		windows.CloseHandle(outcome.thread)
		if err != nil {
			if outcome.desktop != nil {
				outcome.desktop.close()
			}
			return nil, err
		}
	}
	return outcome.desktop, outcome.err
}

func createPrivateDesktopOnThread() (*privateDesktop, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	name := "Wininit-" + hex.EncodeToString(random)
	encoded, _ := windows.UTF16PtrFromString(name)
	// Restricting SIDs/low integrity cannot use the inherited host desktop.
	// Authenticated Users permits both ACL passes; grant access only to fresh
	// noninteractive UI objects, never to the parent's station or desktop.
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;AU)S:(ML;;NW;;;LW)")
	if err != nil {
		return nil, err
	}
	attributes := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	station, _, callErr := createWindowStation.Call(uintptr(unsafe.Pointer(encoded)), 0, 0x37f, uintptr(unsafe.Pointer(&attributes)))
	if station == 0 {
		return nil, fmt.Errorf("create private window station: %w", callErr)
	}
	result := &privateDesktop{name: name + "\\Plugin", station: windows.Handle(station)}
	original, _, callErr := getProcessWindowStation.Call()
	if original == 0 {
		result.close()
		return nil, fmt.Errorf("get parent window station: %w", callErr)
	}
	ok, _, callErr := setProcessWindowStation.Call(station)
	if ok == 0 {
		result.close()
		return nil, fmt.Errorf("select private window station: %w", callErr)
	}
	desktopName, _ := windows.UTF16PtrFromString("Plugin")
	desktop, _, createErr := createDesktop.Call(uintptr(unsafe.Pointer(desktopName)), 0, 0, 0, 0x1ff, uintptr(unsafe.Pointer(&attributes)))
	result.desktop = windows.Handle(desktop)
	restored, _, restoreErr := setProcessWindowStation.Call(original)
	if restored == 0 {
		return result, errors.Join(errors.New("restore parent window station failed"), restoreErr)
	}
	if desktop == 0 {
		result.close()
		return nil, fmt.Errorf("create private desktop: %w", createErr)
	}
	return result, nil
}
func (d *privateDesktop) close() {
	if d.desktop != 0 {
		closeDesktop.Call(uintptr(d.desktop))
		d.desktop = 0
	}
	if d.station != 0 {
		closeWindowStation.Call(uintptr(d.station))
		d.station = 0
	}
}
