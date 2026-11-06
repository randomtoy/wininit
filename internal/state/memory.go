package state

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

type MemoryStore struct {
	mu    sync.RWMutex
	value State
	set   bool
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// BootID is a stable test-only boot identity; this store is never production.
func (*MemoryStore) BootID(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte("memory-store-test-boot"))
	return hex.EncodeToString(sum[:]), nil
}

func (m *MemoryStore) Load(ctx context.Context) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.set {
		return State{}, ErrNotFound
	}
	return clone(m.value), nil
}

func (m *MemoryStore) Save(ctx context.Context, value State) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := value.Validate(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.set {
		value.Generation = m.value.Generation + 1
	} else {
		value.Generation = 1
	}
	m.value = clone(value)
	m.set = true
	return nil
}

func clone(value State) State {
	if value.Executions != nil {
		entries := make(map[string]Execution, len(value.Executions))
		for key, execution := range value.Executions {
			entries[key] = execution
		}
		value.Executions = entries
	}
	if value.Plugins != nil {
		plugins := make(map[string]PluginState, len(value.Plugins))
		for name, checkpoint := range value.Plugins {
			plugins[name] = checkpoint
		}
		value.Plugins = plugins
	}
	if value.Attempts != nil {
		attempts := make(map[string]int, len(value.Attempts))
		for k, v := range value.Attempts {
			attempts[k] = v
		}
		value.Attempts = attempts
	}
	if value.LastError != nil {
		errCopy := *value.LastError
		value.LastError = &errCopy
	}
	return value
}
