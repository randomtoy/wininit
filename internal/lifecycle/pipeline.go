package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/state"
)

func (e *Engine) save(ctx context.Context, current *state.State) error {
	current.UpdatedAt = e.now().UTC()
	return e.store.Save(ctx, *current)
}

// reconcile runs only enabled registrations. Every mutation is preceded by a
// durable running checkpoint, and pending operations are reconciled against
// the real platform on every restart rather than blindly replayed or skipped.
func (e *Engine) reconcile(ctx context.Context, current *state.State) error {
	if current.Plugins == nil {
		current.Plugins = make(map[string]state.PluginState)
	}
	if current.Attempts == nil {
		current.Attempts = make(map[string]int)
	}
	current.ConfigurationHash = e.configurationHash
	current.Provisioning = "disabled"
	current.Checkpoint = ""
	enabledProvisioners := false
	for _, registration := range e.catalog.Provisioners {
		if registration.Enabled {
			enabledProvisioners = true
			current.Provisioning = "no_datasource"
		} else {
			current.Plugins[registration.Plugin.Name()] = state.PluginState{Status: "disabled"}
		}
	}
	var metadata plugin.Metadata
	found := false
	for _, registration := range e.catalog.Datasources {
		name := registration.Plugin.Name()
		if !registration.Enabled {
			current.Plugins[name] = state.PluginState{Status: "disabled"}
			continue
		}
		current.Provisioning = "no_datasource"
		current.Plugins[name] = state.PluginState{Status: "running"}
		current.Attempts[name]++
		if err := e.save(ctx, current); err != nil {
			return err
		}
		candidate, err := registration.Plugin.Discover(ctx)
		if errors.Is(err, plugin.ErrUnavailable) {
			current.Plugins[name] = state.PluginState{Status: "skipped"}
			continue
		}
		if err != nil {
			return e.pluginFailure(ctx, current, name, err)
		}
		metadata, found = candidate, true
		current.Plugins[name] = state.PluginState{Status: "completed"}
		break
	}
	if !found {
		return e.save(ctx, current)
	}
	if current.InstanceID != metadata.InstanceID {
		// A changed instance starts a fresh provisioner history. Disabled plugin
		// checkpoints never carry execution intent into the new instance.
		current.InstanceID = metadata.InstanceID
		for _, registration := range e.catalog.Provisioners {
			delete(current.Plugins, registration.Plugin.Name())
			delete(current.Attempts, registration.Plugin.Name())
			if !registration.Enabled {
				current.Plugins[registration.Plugin.Name()] = state.PluginState{Status: "disabled"}
			}
		}
	}
	current.Provisioning = "metadata_available"
	if !enabledProvisioners {
		return e.save(ctx, current)
	}
	current.Provisioning = "running"
	anyCompleted, anyRebootRequired := false, false
	for _, registration := range e.catalog.Provisioners {
		name := registration.Plugin.Name()
		if !registration.Enabled {
			current.Plugins[name] = state.PluginState{Status: "disabled"}
			continue
		}
		sum := sha256.Sum256([]byte(metadata.InstanceID + "\x00" + name + "\x00" + metadata.LocalHostname))
		checkpoint := state.PluginState{Status: "running", InputHash: hex.EncodeToString(sum[:])}
		current.Plugins[name] = checkpoint
		current.Attempts[name]++
		if err := e.save(ctx, current); err != nil {
			return err
		}
		result, err := registration.Plugin.Apply(ctx, metadata)
		if err != nil {
			return e.pluginFailure(ctx, current, name, err)
		}
		checkpoint.Status = "completed"
		if result.Skipped {
			checkpoint.Status = "skipped"
		} else {
			anyCompleted = true
		}
		checkpoint.RebootRequired = result.RebootRequired
		if result.RebootRequired {
			anyRebootRequired = true
			checkpoint.Status = "reboot_required"
			current.Provisioning = "reboot_required"
			current.Checkpoint = name
		}
		current.Plugins[name] = checkpoint
		if err := e.save(ctx, current); err != nil {
			return err
		}
		e.logger.InfoContext(ctx, "provisioner reconciled", "plugin", name, "changed", result.Changed, "reboot_required", result.RebootRequired)
	}
	switch {
	case anyRebootRequired:
		current.Provisioning = "reboot_required"
	case anyCompleted:
		current.Provisioning = "completed"
	default:
		current.Provisioning = "no_actions"
	}
	return e.save(ctx, current)
}

func (e *Engine) pluginFailure(ctx context.Context, current *state.State, name string, cause error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	checkpoint := current.Plugins[name]
	checkpoint.Status = "failed"
	current.Plugins[name] = checkpoint
	current.Provisioning = "failed"
	// Untrusted plugin error text is intentionally absent from durable state.
	current.LastError = &state.ErrorMetadata{Code: "plugin_failed", Message: "Plugin operation failed; review the service log", OccurredAt: e.now().UTC()}
	current.Phase = state.PhaseFoundationError
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := e.save(saveCtx, current); err != nil {
		return errors.Join(fmt.Errorf("plugin %s failed: %w", name, cause), err)
	}
	return fmt.Errorf("plugin %s failed: %w", name, cause)
}
