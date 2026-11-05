//go:build !windows

package state

import "context"

type RegistryStore struct{}

func NewRegistryStore(_ string) *RegistryStore { return &RegistryStore{} }

func (*RegistryStore) Load(context.Context) (State, error) { return State{}, ErrUnsupported }

func (*RegistryStore) Save(context.Context, State) error { return ErrUnsupported }
