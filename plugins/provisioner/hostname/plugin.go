// Package hostname implements idempotent Windows computer-name provisioning.
package hostname

import (
	"context"
	"errors"
	"fmt"
	"strings"

	pluginapi "github.com/randomtoy/wininit/internal/plugin"
)

const name = "hostname"

// Platform isolates the operating-system operations needed by the provisioner.
// Snapshot returns the name active in the current boot and the configured name
// that will become active after a restart.
type Platform interface {
	Snapshot(ctx context.Context) (active string, configured string, err error)
	Set(ctx context.Context, hostname string) error
}

// Plugin applies a metadata-provided computer name through a Platform.
type Plugin struct {
	platform Platform
}

var _ pluginapi.Provisioner = (*Plugin)(nil)

// New constructs the hostname provisioner.
func New(platform Platform) *Plugin {
	return &Plugin{platform: platform}
}

// Name returns the stable provisioner identifier.
func (p *Plugin) Name() string {
	return name
}

// Apply configures the requested local hostname when necessary. A configured
// name that is not active yet represents a pending reboot, not another change.
func (p *Plugin) Apply(ctx context.Context, metadata pluginapi.Metadata) (pluginapi.Result, error) {
	if err := ctx.Err(); err != nil {
		return pluginapi.Result{}, err
	}

	desired := metadata.LocalHostname
	if desired == "" {
		return pluginapi.Result{Skipped: true}, nil
	}
	if err := validate(desired); err != nil {
		return pluginapi.Result{}, fmt.Errorf("invalid local hostname: %w", err)
	}
	if p == nil || p.platform == nil {
		return pluginapi.Result{}, errors.New("hostname platform is not configured")
	}

	active, configured, err := p.platform.Snapshot(ctx)
	if err != nil {
		return pluginapi.Result{}, fmt.Errorf("read hostname state: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return pluginapi.Result{}, err
	}

	activeMatches := strings.EqualFold(active, desired)
	configuredMatches := strings.EqualFold(configured, desired)
	switch {
	case activeMatches && configuredMatches:
		return pluginapi.Result{}, nil
	case configuredMatches:
		return pluginapi.Result{RebootRequired: true}, nil
	default:
		if err := p.platform.Set(ctx, desired); err != nil {
			return pluginapi.Result{}, fmt.Errorf("set hostname: %w", err)
		}
		return pluginapi.Result{Changed: true, RebootRequired: true}, nil
	}
}

func validate(hostname string) error {
	if len(hostname) < 1 || len(hostname) > 15 {
		return errors.New("must contain between 1 and 15 ASCII characters")
	}
	if !isASCIIAlphanumeric(hostname[0]) || !isASCIIAlphanumeric(hostname[len(hostname)-1]) {
		return errors.New("must start and end with an ASCII letter or digit")
	}

	hasLetter := false
	for i := 0; i < len(hostname); i++ {
		character := hostname[i]
		switch {
		case isASCIILetter(character):
			hasLetter = true
		case character >= '0' && character <= '9':
		case character == '-':
		default:
			return errors.New("may contain only ASCII letters, digits, and hyphens")
		}
	}
	if !hasLetter {
		return errors.New("must contain at least one ASCII letter")
	}
	return nil
}

func isASCIIAlphanumeric(character byte) bool {
	return isASCIILetter(character) || character >= '0' && character <= '9'
}

func isASCIILetter(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}
