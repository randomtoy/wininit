//go:build windows

package identity

import (
	"context"
	"fmt"
	"github.com/randomtoy/wininit/internal/identity"
	"golang.org/x/sys/windows"
	"unsafe"
)

var netUserModalsGet = netapi.NewProc("NetUserModalsGet")
var netUserModalsSet = netapi.NewProc("NetUserModalsSet")

type modals0 struct{ MinLength, MaxAge, MinAge, ForceLogoff, History uint32 }
type modals3 struct{ Duration, Window, Threshold uint32 }

func readModals(level uintptr, target unsafe.Pointer, size uintptr) error {
	var buffer *byte
	code, _, _ := netUserModalsGet.Call(0, level, uintptr(unsafe.Pointer(&buffer)))
	if code != 0 {
		return fmt.Errorf("read local password policy failed (code %d)", code)
	}
	defer windows.NetApiBufferFree(buffer)
	copy(unsafe.Slice((*byte)(target), int(size)), unsafe.Slice(buffer, int(size)))
	return nil
}
func (*Adapter) PasswordPolicy(ctx context.Context) (identity.PasswordPolicy, error) {
	if err := localOnly(ctx); err != nil {
		return identity.PasswordPolicy{}, err
	}
	var base modals0
	var lockout modals3
	if err := readModals(0, unsafe.Pointer(&base), unsafe.Sizeof(base)); err != nil {
		return identity.PasswordPolicy{}, err
	}
	if err := readModals(3, unsafe.Pointer(&lockout), unsafe.Sizeof(lockout)); err != nil {
		return identity.PasswordPolicy{}, err
	}
	return identity.PasswordPolicy{
		MinimumLength: int(base.MinLength), MaximumAgeDays: exactUnits(base.MaxAge, 86400), MinimumAgeDays: exactUnits(base.MinAge, 86400), HistoryLength: int(base.History),
		LockoutThreshold: int(lockout.Threshold), LockoutDurationMinutes: exactUnits(lockout.Duration, 60), LockoutWindowMinutes: exactUnits(lockout.Window, 60),
	}, nil
}

func exactUnits(value, unit uint32) int {
	if value%unit != 0 {
		return -1
	}
	return int(value / unit)
}
func (*Adapter) SetPasswordPolicy(ctx context.Context, desired identity.PasswordPolicy) error {
	if err := localOnly(ctx); err != nil {
		return err
	}
	if err := desired.Validate(); err != nil {
		return err
	}
	var base modals0
	if err := readModals(0, unsafe.Pointer(&base), unsafe.Sizeof(base)); err != nil {
		return err
	}
	base.MinLength, base.MaxAge, base.MinAge, base.History = uint32(desired.MinimumLength), uint32(desired.MaximumAgeDays*86400), uint32(desired.MinimumAgeDays*86400), uint32(desired.HistoryLength)
	lockout := modals3{uint32(desired.LockoutDurationMinutes * 60), uint32(desired.LockoutWindowMinutes * 60), uint32(desired.LockoutThreshold)}
	for _, field := range []struct {
		level uintptr
		value unsafe.Pointer
	}{{0, unsafe.Pointer(&base)}, {3, unsafe.Pointer(&lockout)}} {
		if err := ctx.Err(); err != nil {
			return err
		}
		var parameter uint32
		code, _, _ := netUserModalsSet.Call(0, field.level, uintptr(field.value), uintptr(unsafe.Pointer(&parameter)))
		if code != 0 {
			return fmt.Errorf("set local password policy failed (code %d)", code)
		}
	}
	return nil
}
