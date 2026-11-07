package pluginprocess

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/randomtoy/wininit/internal/lifecycle"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/state"
)

// The datasource already failed on its private limit. Delay only the terminal
// save response until the real root deadline expires, reproducing slow Registry
// persistence concurrent with lifecycle termination without a fake plugin error.
type deadlineAfterDatasourceFailure struct {
	*state.MemoryStore
	root context.Context
}

func (s deadlineAfterDatasourceFailure) Save(ctx context.Context, value state.State) error {
	if err := s.MemoryStore.Save(ctx, value); err != nil {
		return err
	}
	if value.Provisioning == "failed" {
		<-s.root.Done()
	}
	return nil
}
func TestDatasourceTimeoutSurvivesConcurrentRootDeadline(t *testing.T) {
	for _, name := range []string{"configdrive", "nocloud"} {
		t.Run(name, func(t *testing.T) {
			mode := "hang"
			if name == "nocloud" {
				mode = "nc-hang"
			}
			settings := processSettings(t, mode)
			settings.TimeoutSeconds = 3
			var source plugin.Datasource = NewConfigDrive(settings, "", false)
			if name == "nocloud" {
				source = NewNoCloud(settings, "", false)
			}
			root, stop := context.WithTimeout(context.Background(), 4*time.Second)
			defer stop()
			memory := state.NewMemoryStore()
			store := deadlineAfterDatasourceFailure{MemoryStore: memory, root: root}
			engine := lifecycle.NewWithPlugins(store, slog.New(slog.NewTextHandler(io.Discard, nil)), plugin.Catalog{Datasources: []plugin.DatasourceRegistration{{Enabled: true, Plugin: source}}}, "review-datasource-deadline")
			err := engine.Run(root)
			if !errors.Is(err, errDatasourceTimeout) || errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("private datasource timeout was mistaken for root cancellation: %v", err)
			}
			saved, loadErr := memory.Load(context.Background())
			if loadErr != nil || saved.Provisioning != "failed" || saved.LastError == nil || saved.Phase != state.PhaseFoundationError || saved.Plugins[name].Status != "failed" {
				t.Fatalf("datasource failure evidence lost: %#v %v", saved, loadErr)
			}
		})
	}
}
