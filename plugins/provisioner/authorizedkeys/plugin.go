// Package authorizedkeys manages a bounded block of SSH public keys for local users.
package authorizedkeys

import (
	"context"
	"errors"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
)

type Platform interface {
	ReconcileKeys(context.Context, string, []string) (bool, error)
}
type Plugin struct {
	users    []identity.KeyUser
	platform Platform
}

func New(users []identity.KeyUser, platform Platform) *Plugin {
	copyUsers := make([]identity.KeyUser, len(users))
	for i, user := range users {
		copyUsers[i] = identity.KeyUser{Name: user.Name, PublicKeys: append([]string(nil), user.PublicKeys...)}
	}
	return &Plugin{users: copyUsers, platform: platform}
}
func (*Plugin) Name() string { return "authorized_keys" }
func (p *Plugin) Apply(ctx context.Context, _ plugin.Metadata) (plugin.Result, error) {
	if err := ctx.Err(); err != nil {
		return plugin.Result{}, err
	}
	if err := identity.ValidateKeyUsers(p.users); err != nil {
		return plugin.Result{}, err
	}
	if p.platform == nil {
		return plugin.Result{}, errors.New("authorized keys platform is not configured")
	}
	changed := false
	for _, user := range p.users {
		if err := ctx.Err(); err != nil {
			return plugin.Result{}, err
		}
		keys, err := identity.NormalizeKeys(user.PublicKeys)
		if err != nil {
			return plugin.Result{}, err
		}
		applied, err := p.platform.ReconcileKeys(ctx, user.Name, keys)
		if err != nil {
			return plugin.Result{}, err
		}
		changed = changed || applied
	}
	return plugin.Result{Changed: changed}, nil
}
