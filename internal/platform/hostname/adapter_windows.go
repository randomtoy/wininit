//go:build windows

package hostname

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const tcpipParametersKey = `SYSTEM\CurrentControlSet\Services\Tcpip\Parameters`

var setComputerNameEx = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetComputerNameExW")

// Snapshot returns the physical DNS hostname active in this boot and the
// pending hostname stored by Windows for the next boot.
func (*Adapter) Snapshot(ctx context.Context) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	active, err := physicalDNSHostname()
	if err != nil {
		return "", "", fmt.Errorf("get active computer name: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}

	key, err := registry.OpenKey(
		registry.LOCAL_MACHINE,
		tcpipParametersKey,
		registry.QUERY_VALUE|registry.WOW64_64KEY,
	)
	if err != nil {
		return "", "", fmt.Errorf("open TCP/IP parameters registry key: %w", err)
	}
	defer key.Close()

	configured, _, err := key.GetStringValue("NV Hostname")
	if err != nil {
		return "", "", fmt.Errorf("read configured computer name: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	return active, configured, nil
}

// Set configures a physical DNS hostname for the next boot. Domain-joined
// computers are rejected because their machine account requires a coordinated
// Active Directory rename rather than a local SetComputerNameEx call.
func (*Adapter) Set(ctx context.Context, hostname string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	joined, err := domainJoined()
	if err != nil {
		return fmt.Errorf("query domain join status: %w", err)
	}
	if joined {
		return errors.New("refusing to rename a domain-joined computer")
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	name, err := windows.UTF16PtrFromString(hostname)
	if err != nil {
		return fmt.Errorf("encode computer name: %w", err)
	}
	result, _, callErr := setComputerNameEx.Call(
		uintptr(windows.ComputerNamePhysicalDnsHostname),
		uintptr(unsafe.Pointer(name)),
	)
	if result == 0 {
		if callErr != windows.ERROR_SUCCESS {
			return callErr
		}
		return errors.New("SetComputerNameExW failed without an error code")
	}
	return nil
}

func physicalDNSHostname() (string, error) {
	size := uint32(256)
	for {
		buffer := make([]uint16, size)
		length := size
		err := windows.GetComputerNameEx(windows.ComputerNamePhysicalDnsHostname, &buffer[0], &length)
		if err == nil {
			return windows.UTF16ToString(buffer[:length]), nil
		}
		if !errors.Is(err, windows.ERROR_MORE_DATA) || length <= size {
			return "", err
		}
		size = length
	}
}

func domainJoined() (bool, error) {
	var name *uint16
	var status uint32
	if err := windows.NetGetJoinInformation(nil, &name, &status); err != nil {
		return false, err
	}
	if name != nil {
		defer windows.NetApiBufferFree((*byte)(unsafe.Pointer(name)))
	}
	return status == windows.NetSetupDomainName, nil
}
