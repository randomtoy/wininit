//go:build windows

package state

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var regFlushKey = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegFlushKey")

const (
	activeValue = "ActiveCheckpoint"
	slotAValue  = "CheckpointA"
	slotBValue  = "CheckpointB"
)

type RegistryStore struct {
	root registry.Key
	path string
}

func NewRegistryStore(path string) *RegistryStore {
	return &RegistryStore{root: registry.LOCAL_MACHINE, path: path}
}

func newRegistryStore(root registry.Key, path string) *RegistryStore {
	return &RegistryStore{root: root, path: path}
}

func (s *RegistryStore) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	key, err := registry.OpenKey(s.root, s.path, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return State{}, ErrNotFound
	}
	if err != nil {
		return State{}, fmt.Errorf("open registry state key: %w", err)
	}
	defer key.Close()
	value, _, ok, recoveryErr := highestValidSlot(key)
	if recoveryErr != nil {
		return State{}, recoveryErr
	}
	active, _, activeErr := key.GetStringValue(activeValue)
	if activeErr == nil {
		if value, err := loadSlot(key, active); err == nil {
			return value, nil
		} else if errors.Is(err, ErrUnsupportedSchema) {
			return State{}, err
		}
	}
	if ok {
		return value, nil
	}
	if errors.Is(activeErr, registry.ErrNotExist) {
		_, _, firstErr := key.GetBinaryValue(slotAValue)
		_, _, secondErr := key.GetBinaryValue(slotBValue)
		if errors.Is(firstErr, registry.ErrNotExist) && errors.Is(secondErr, registry.ErrNotExist) {
			return State{}, ErrNotFound
		}
	}
	return State{}, fmt.Errorf("%w: no valid Registry checkpoint", ErrCorrupt)
}

func (s *RegistryStore) Save(ctx context.Context, value State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	key, _, err := registry.CreateKey(s.root, s.path, registry.ALL_ACCESS)
	if err != nil {
		return fmt.Errorf("create registry state key: %w", err)
	}
	defer key.Close()
	previous, bestSlot, hasPrevious, recoveryErr := highestValidSlot(key)
	if recoveryErr != nil {
		return recoveryErr
	}
	active, _, err := key.GetStringValue(activeValue)
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("read active checkpoint marker: %w", err)
	}
	if _, activeErr := loadSlot(key, active); activeErr != nil {
		if hasPrevious {
			active = bestSlot
		} else {
			active = slotBValue
		}
	}
	target := slotAValue
	if active == slotAValue {
		target = slotBValue
	}
	if hasPrevious {
		value.Generation = previous.Generation + 1
	} else {
		value.Generation = 1
	}
	data, err := encodeCheckpoint(value)
	if err != nil {
		return err
	}
	if err := key.SetBinaryValue(target, data); err != nil {
		return fmt.Errorf("write inactive registry checkpoint: %w", err)
	}
	verified, err := loadSlot(key, target)
	if err != nil || verified.Generation != value.Generation {
		return fmt.Errorf("verify inactive registry checkpoint: %w", errors.Join(err, ErrCorrupt))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := key.SetStringValue(activeValue, target); err != nil {
		return fmt.Errorf("activate registry checkpoint: %w", err)
	}
	// The lifecycle relies on Save returning before a platform mutation. Flush
	// the committed marker and checkpoint to disk rather than only the Registry
	// cache. This is expensive but saves occur only at lifecycle boundaries.
	result, _, _ := regFlushKey.Call(uintptr(key))
	if result != 0 {
		return fmt.Errorf("flush registry checkpoint: %w", syscall.Errno(result))
	}
	return nil
}

func loadSlot(key registry.Key, name string) (State, error) {
	if name != slotAValue && name != slotBValue {
		return State{}, fmt.Errorf("%w: invalid checkpoint marker %q", ErrCorrupt, name)
	}
	data, _, err := key.GetBinaryValue(name)
	if err != nil {
		return State{}, err
	}
	return decodeCheckpoint(data)
}

func highestValidSlot(key registry.Key) (State, string, bool, error) {
	var best State
	var bestSlot string
	var found bool
	for _, slot := range []string{slotAValue, slotBValue} {
		value, err := loadSlot(key, slot)
		if errors.Is(err, ErrUnsupportedSchema) {
			return State{}, "", false, err
		}
		if err == nil && (!found || value.Generation > best.Generation) {
			best, bestSlot, found = value, slot, true
		}
	}
	return best, bestSlot, found, nil
}
