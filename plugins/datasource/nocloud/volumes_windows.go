//go:build windows

package nocloud

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

var setThreadErrorMode = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadErrorMode")

// FindVolumes discovers mounted FAT/ISO9660 CIDATA volumes without reading
// other drives' contents. SMB and other remote drives are never considered.
func FindVolumes(ctx context.Context) ([]string, error) {
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
		var label, filesystem [256]uint16
		err = windows.GetVolumeInformation(path, &label[0], uint32(len(label)), nil, nil, nil, &filesystem[0], uint32(len(filesystem)))
		if errors.Is(err, windows.ERROR_NOT_READY) || errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("query local volume: %w", err)
		}
		if !strings.EqualFold(windows.UTF16ToString(label[:]), "cidata") {
			continue
		}
		switch strings.ToUpper(windows.UTF16ToString(filesystem[:])) {
		case "FAT", "FAT32", "CDFS", "ISO9660":
			roots = append(roots, root)
		default:
			return nil, errors.New("CIDATA volume requires FAT or ISO9660 filesystem")
		}
	}
	return roots, nil
}
