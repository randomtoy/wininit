//go:build !windows

package identity

import (
	"context"
	"errors"
	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/plugins/provisioner/localusers"
)

func unsupported(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("local identity provisioning requires Windows")
}
func (*Adapter) User(ctx context.Context, _ string) (localusers.UserState, bool, error) {
	return localusers.UserState{}, false, unsupported(ctx)
}
func (*Adapter) ReadPassword(ctx context.Context, _ string) ([]byte, error) {
	return nil, unsupported(ctx)
}
func (*Adapter) CreateUser(ctx context.Context, _ identity.User, _ []byte) error {
	return unsupported(ctx)
}
func (*Adapter) UpdateUser(ctx context.Context, _ identity.User) error { return unsupported(ctx) }
func (*Adapter) PasswordPolicy(ctx context.Context) (identity.PasswordPolicy, error) {
	return identity.PasswordPolicy{}, unsupported(ctx)
}
func (*Adapter) SetPasswordPolicy(ctx context.Context, _ identity.PasswordPolicy) error {
	return unsupported(ctx)
}
func (*Adapter) ReconcileKeys(ctx context.Context, _ string, _ []string) (bool, error) {
	return false, unsupported(ctx)
}
