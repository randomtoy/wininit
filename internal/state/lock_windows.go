//go:build windows

package state

import (
	"context"
	"errors"

	"golang.org/x/sys/windows"
)

// AcquireMachineLock excludes concurrent mutation by service/console processes,
// including instances configured with different state keys. Holding the named
// object handle (no thread-affine mutex ownership) provides lifetime exclusion.
func AcquireMachineLock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name, err := windows.UTF16PtrFromString(`Global\WininitProvisioning`)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		if handle != 0 {
			windows.CloseHandle(handle)
		}
		return nil, errors.New("another wininit lifecycle process is running")
	}
	if err != nil {
		return nil, err
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}
