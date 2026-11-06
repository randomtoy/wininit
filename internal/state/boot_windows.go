//go:build windows

package state

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// BootID is a protected volatile marker, stable across service/process starts
// but discarded by Windows at the next kernel boot. The provisioning mutex
// serializes creation. It is deliberately not part of the durable checkpoint.
func (s *RegistryStore) BootID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	parent, err := registry.OpenKey(s.root, s.path, registry.CREATE_SUB_KEY|registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("open boot marker parent: %w", err)
	}
	defer parent.Close()
	sd, err := windows.SecurityDescriptorFromString("O:BAG:BAD:P(A;;KA;;;SY)(A;;KA;;;BA)")
	if err != nil {
		return "", err
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	name, _ := windows.UTF16PtrFromString("BootSession")
	var handle windows.Handle
	var disposition uint32
	proc := windows.NewLazySystemDLL("advapi32.dll").NewProc("RegCreateKeyExW")
	code, _, _ := proc.Call(uintptr(parent), uintptr(unsafe.Pointer(name)), 0, 0, 1, registry.ALL_ACCESS, uintptr(unsafe.Pointer(&sa)), uintptr(unsafe.Pointer(&handle)), uintptr(unsafe.Pointer(&disposition)))
	if code != 0 {
		return "", fmt.Errorf("create volatile boot marker: %w", syscall.Errno(code))
	}
	key := registry.Key(handle)
	defer key.Close()
	if disposition == 1 {
		data := make([]byte, 32)
		if _, err := rand.Read(data); err != nil {
			return "", err
		}
		if err := key.SetStringValue("ID", hex.EncodeToString(data)); err != nil {
			return "", err
		}
	}
	value, _, err := key.GetStringValue("ID")
	if err != nil || !ValidHash(value) {
		return "", errors.New("invalid volatile boot marker")
	}
	return value, ctx.Err()
}
