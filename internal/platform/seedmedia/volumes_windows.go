//go:build windows

package seedmedia

import (
	"context"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var setThreadErrorMode = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadErrorMode")

// FindVolumes discovers mounted FAT/ISO9660 volumes with the requested label without reading
// other drives' contents. SMB and other remote drives are never considered.
func FindVolumes(ctx context.Context, label string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Empty CD-ROM/removable media must not show an interactive "insert disk"
	// dialog in a service. This mode belongs to the OS thread, so pin it until
	// the old mode is restored rather than changing the whole process.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var oldMode uint32
	result, _, modeErr := setThreadErrorMode.Call(uintptr(windows.SEM_FAILCRITICALERRORS), uintptr(unsafe.Pointer(&oldMode)))
	if result == 0 {
		return nil, fmt.Errorf("suppress interactive volume errors: %w", modeErr)
	}
	defer setThreadErrorMode.Call(uintptr(oldMode), 0)
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil, fmt.Errorf("enumerate local drive letters: %w", err)
	}
	var roots []string
	for index := 0; index < 26; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if mask&(1<<index) == 0 {
			continue
		}
		root := string(rune('A'+index)) + `:\`
		path, err := windows.UTF16PtrFromString(root)
		if err != nil {
			return nil, err
		}
		switch windows.GetDriveType(path) {
		case windows.DRIVE_REMOVABLE, windows.DRIVE_FIXED, windows.DRIVE_CDROM:
		default:
			continue
		}
		var volumeLabel, filesystem [256]uint16
		err = windows.GetVolumeInformation(path, &volumeLabel[0], uint32(len(volumeLabel)), nil, nil, nil, &filesystem[0], uint32(len(filesystem)))
		if err != nil {
			// Inaccessible or empty unrelated drives do not invalidate another
			// eligible seed. No directory contents are probed during enumeration.
			continue
		}
		if matchesVolume(windows.UTF16ToString(volumeLabel[:]), windows.UTF16ToString(filesystem[:]), label) {
			roots = append(roots, root)
		}
	}
	return roots, nil
}
