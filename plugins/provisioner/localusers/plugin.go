// Package localusers creates and reconciles explicitly configured standard local accounts.
package localusers

import (
	"context"
	"errors"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
)

type UserState struct {
	FullName, Description string
	PasswordNeverExpires  bool
}
type Platform interface {
	User(context.Context, string) (UserState, bool, error)
	ReadPassword(context.Context, string) ([]byte, error)
	CreateUser(context.Context, identity.User, []byte) error
	UpdateUser(context.Context, identity.User) error
}
type Plugin struct {
	users    []identity.User
	platform Platform
}

func New(users []identity.User, platform Platform) *Plugin {
	return &Plugin{users: append([]identity.User(nil), users...), platform: platform}
}
func (*Plugin) Name() string { return "local_users" }
func (p *Plugin) Apply(ctx context.Context, _ plugin.Metadata) (plugin.Result, error) {
	if err := ctx.Err(); err != nil {
		return plugin.Result{}, err
	}
	if err := identity.ValidateUsers(p.users); err != nil {
		return plugin.Result{}, err
	}
	if p.platform == nil {
		return plugin.Result{}, errors.New("local user platform is not configured")
	}
	changed := false
	for _, user := range p.users {
		if err := ctx.Err(); err != nil {
			return plugin.Result{}, err
		}
		current, exists, err := p.platform.User(ctx, user.Name)
		if err != nil {
			return plugin.Result{}, err
		}
		if !exists {
			password, err := p.platform.ReadPassword(ctx, user.PasswordFile)
			if err != nil {
				return plugin.Result{}, err
			}
			err = p.platform.CreateUser(ctx, user, password)
			clear(password)
			if err != nil {
				return plugin.Result{}, err
			}
			changed = true
		} else if current != (UserState{FullName: user.FullName, Description: user.Description, PasswordNeverExpires: user.PasswordNeverExpires}) {
			if err := p.platform.UpdateUser(ctx, user); err != nil {
				return plugin.Result{}, err
			}
			changed = true
		}
	}
	return plugin.Result{Changed: changed}, nil
}
