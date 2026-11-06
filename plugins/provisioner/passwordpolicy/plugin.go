// Package passwordpolicy reconciles the local SAM password and lockout policy.
package passwordpolicy

import (
	"context"
	"errors"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
)

type Platform interface {
	PasswordPolicy(context.Context) (identity.PasswordPolicy, error)
	SetPasswordPolicy(context.Context, identity.PasswordPolicy) error
}
type Plugin struct {
	desired  identity.PasswordPolicy
	platform Platform
}

func New(desired identity.PasswordPolicy, platform Platform) *Plugin {
	return &Plugin{desired: desired, platform: platform}
}
func (*Plugin) Name() string { return "password_policy" }
func (p *Plugin) Apply(ctx context.Context, _ plugin.Metadata) (plugin.Result, error) {
	if err := ctx.Err(); err != nil {
		return plugin.Result{}, err
	}
	if err := p.desired.Validate(); err != nil {
		return plugin.Result{}, err
	}
	if p.platform == nil {
		return plugin.Result{}, errors.New("password policy platform is not configured")
	}
	current, err := p.platform.PasswordPolicy(ctx)
	if err != nil {
		return plugin.Result{}, err
	}
	if current == p.desired {
		return plugin.Result{}, nil
	}
	if err := p.platform.SetPasswordPolicy(ctx, p.desired); err != nil {
		return plugin.Result{}, err
	}
	return plugin.Result{Changed: true}, nil
}
