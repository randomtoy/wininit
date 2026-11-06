package lifecycle

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/state"
	"github.com/randomtoy/wininit/plugins/datasource/configdrive"
	"github.com/randomtoy/wininit/plugins/datasource/nocloud"
	hostnameplugin "github.com/randomtoy/wininit/plugins/provisioner/hostname"
)

type testHostnamePlatform struct {
	active, configured string
	writes             int
}

func TestConfigDriveFallbackToHostnameAndResume(t *testing.T) {
	t.Parallel()
	seed, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "configdrive"))
	if err != nil {
		t.Fatal(err)
	}
	engine, store, noCloud, unusedProvisioner := pipeline(t, true, true)
	noCloud.err = plugin.ErrUnavailable
	platform := &testHostnamePlatform{active: "old-node", configured: "old-node"}
	engine.catalog.Datasources = append(engine.catalog.Datasources, plugin.DatasourceRegistration{Enabled: true, Plugin: configdrive.New(seed, false, nil)})
	engine.catalog.Provisioners = []plugin.ProvisionerRegistration{{Enabled: true, Plugin: hostnameplugin.New(platform)}}
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if current.InstanceID != "5fbd2b18-441d-46b0-9efa-18e270af77d5" || current.Provisioning != "reboot_required" || platform.configured != "wininit-node" || platform.writes != 1 || noCloud.calls != 1 || unusedProvisioner.calls != 0 {
		t.Fatalf("state=%#v platform=%#v", current, platform)
	}
	current, err = store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	platform.active = platform.configured
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Provisioning != "completed" || platform.writes != 1 {
		t.Fatalf("resumed state=%#v writes=%d", current, platform.writes)
	}
}

func TestDisabledConfigDriveNeverDiscoversOrResumes(t *testing.T) {
	t.Parallel()
	engine, _, _, provisioner := pipeline(t, false, true)
	engine.catalog.Datasources = append(engine.catalog.Datasources, plugin.DatasourceRegistration{Enabled: false, Plugin: configdrive.New("", true, func(context.Context) ([]string, error) {
		t.Fatal("disabled ConfigDrive enumerated drives")
		return nil, nil
	})})
	current := state.Initial(time.Now())
	current.Plugins = map[string]state.PluginState{"configdrive": {Status: "completed"}}
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if provisioner.calls != 0 || current.Plugins["configdrive"].Status != "disabled" || current.Provisioning != "no_datasource" {
		t.Fatalf("state=%#v provisioner calls=%d", current, provisioner.calls)
	}
}

type configDriveCounter struct{ countingDatasource }

func (*configDriveCounter) Name() string { return "configdrive" }

func TestDatasourceSelectionNeverDependsOnCatalogOrder(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		for _, instanceID := range []string{"vm-1", "other-instance"} {
			engine, store, noCloud, provisioner := pipeline(t, true, true)
			configDrive := &configDriveCounter{countingDatasource: countingDatasource{metadata: plugin.Metadata{InstanceID: instanceID, LocalHostname: "other-node"}}}
			engine.catalog.Datasources = append(engine.catalog.Datasources, plugin.DatasourceRegistration{Enabled: true, Plugin: configDrive})
			if reverse {
				engine.catalog.Datasources[0], engine.catalog.Datasources[1] = engine.catalog.Datasources[1], engine.catalog.Datasources[0]
			}
			current := state.Initial(time.Now())
			current.InstanceID = "previous-instance"
			if err := engine.reconcile(context.Background(), &current); err == nil {
				t.Fatal("multiple datasources accepted")
			}
			if provisioner.calls != 0 || noCloud.calls != 1 || configDrive.calls != 1 || current.InstanceID != "previous-instance" {
				t.Fatalf("ambiguous sources changed state or ran provisioner: %#v calls=%d", current, provisioner.calls)
			}
			failed, err := store.Load(context.Background())
			if err != nil || failed.Provisioning != "failed" {
				t.Fatalf("missing durable selection failure: %#v %v", failed, err)
			}
		}
	}
}

func TestMalformedDatasourceDoesNotFallBackToAnother(t *testing.T) {
	t.Parallel()
	engine, _, noCloud, provisioner := pipeline(t, true, true)
	want := errors.New("malformed NoCloud metadata")
	noCloud.err = want
	configDrive := &configDriveCounter{}
	engine.catalog.Datasources = append(engine.catalog.Datasources, plugin.DatasourceRegistration{Enabled: true, Plugin: configDrive})
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); !errors.Is(err, want) {
		t.Fatalf("error=%v", err)
	}
	if configDrive.calls != 0 || provisioner.calls != 0 {
		t.Fatal("invalid metadata was bypassed")
	}
}

func TestUnavailableSecondDatasourceDoesNotMaskAvailableFirst(t *testing.T) {
	t.Parallel()
	engine, _, noCloud, provisioner := pipeline(t, true, true)
	configDrive := &configDriveCounter{countingDatasource: countingDatasource{err: plugin.ErrUnavailable}}
	engine.catalog.Datasources = append(engine.catalog.Datasources, plugin.DatasourceRegistration{Enabled: true, Plugin: configDrive})
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if current.InstanceID != noCloud.metadata.InstanceID || current.Provisioning != "completed" || configDrive.calls != 1 || provisioner.calls != 1 {
		t.Fatalf("state=%#v", current)
	}
}

func (p *testHostnamePlatform) Snapshot(context.Context) (string, string, error) {
	return p.active, p.configured, nil
}
func (p *testHostnamePlatform) Set(_ context.Context, name string) error {
	p.configured = name
	p.writes++
	return nil
}

func TestNoCloudToHostnamePersistsAndResumesAcrossBoots(t *testing.T) {
	t.Parallel()
	seed, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "nocloud"))
	if err != nil {
		t.Fatal(err)
	}
	platform := &testHostnamePlatform{active: "old-node", configured: "old-node"}
	catalog := plugin.Catalog{Datasources: []plugin.DatasourceRegistration{{Enabled: true, Plugin: nocloud.New(seed, false, nil)}}, Provisioners: []plugin.ProvisionerRegistration{{Enabled: true, Plugin: hostnameplugin.New(platform)}}}
	store := state.NewMemoryStore()
	engine := NewWithPlugins(store, slog.New(slog.NewJSONHandler(io.Discard, nil)), catalog, "config")
	current := state.Initial(time.Now())
	for attempt := 0; attempt < 2; attempt++ {
		if err := engine.reconcile(context.Background(), &current); err != nil {
			t.Fatal(err)
		}
		if current.Provisioning != "reboot_required" || platform.writes != 1 {
			t.Fatalf("attempt %d: state=%s writes=%d", attempt, current.Provisioning, platform.writes)
		}
		current, err = store.Load(context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	// Simulate the OS activating its configured hostname after a real reboot.
	platform.active = platform.configured
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Provisioning != "completed" || platform.writes != 1 || current.InstanceID != "wininit-fixture-001" {
		t.Fatalf("resumed state=%#v writes=%d", current, platform.writes)
	}
}

type cancelingDatasource struct{ cancel context.CancelFunc }

func (*cancelingDatasource) Name() string { return "canceling" }
func (d *cancelingDatasource) Discover(ctx context.Context) (plugin.Metadata, error) {
	d.cancel()
	return plugin.Metadata{}, ctx.Err()
}

func TestStopDuringPluginRunIsGracefulAndRetryable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := state.NewMemoryStore()
	catalog := plugin.Catalog{Datasources: []plugin.DatasourceRegistration{{Enabled: true, Plugin: &cancelingDatasource{cancel: cancel}}}}
	engine := NewWithPlugins(store, slog.New(slog.NewJSONHandler(io.Discard, nil)), catalog, "config")
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("graceful stop failed: %v", err)
	}
	current, err := store.Load(context.Background())
	if err != nil || current.Phase != state.PhaseAgentStopped || current.Provisioning != "pending" || current.LastError != nil {
		t.Fatalf("state=%#v err=%v", current, err)
	}
}

type countingDatasource struct {
	calls    int
	metadata plugin.Metadata
	err      error
	before   func()
}

func (*countingDatasource) Name() string { return "nocloud" }
func (d *countingDatasource) Discover(context.Context) (plugin.Metadata, error) {
	d.calls++
	if d.before != nil {
		d.before()
	}
	return d.metadata, d.err
}

func TestDiscoveryIsPendingUntilSourceAbsenceIsKnown(t *testing.T) {
	t.Parallel()
	engine, store, datasource, _ := pipeline(t, true, true)
	datasource.err = plugin.ErrUnavailable
	datasource.before = func() {
		current, err := store.Load(context.Background())
		if err != nil || current.Provisioning != "pending" {
			t.Fatalf("discovery reported absence prematurely: %#v %v", current, err)
		}
	}
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Provisioning != "no_datasource" {
		t.Fatalf("state=%#v", current)
	}
}

func TestConcurrentStopDoesNotErasePluginFailure(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine, store, _, provisioner := pipeline(t, true, true)
	want := errors.New("actual platform error")
	provisioner.before, provisioner.err = cancel, want
	if err := engine.Run(ctx); !errors.Is(err, want) {
		t.Fatalf("Run() error=%v", err)
	}
	current, err := store.Load(context.Background())
	if err != nil || current.Provisioning != "failed" || current.LastError == nil || current.Phase != state.PhaseFoundationError {
		t.Fatalf("failure overwritten: %#v %v", current, err)
	}
}

type countingProvisioner struct {
	calls  int
	result plugin.Result
	err    error
	before func()
}

type rejectMutationCheckpoint struct{ *state.MemoryStore }

func (s rejectMutationCheckpoint) Save(ctx context.Context, current state.State) error {
	if current.Plugins["hostname"].Status == "running" {
		return errors.New("access denied while checkpointing hostname")
	}
	return s.MemoryStore.Save(ctx, current)
}

func (*countingProvisioner) Name() string { return "hostname" }
func (p *countingProvisioner) Apply(context.Context, plugin.Metadata) (plugin.Result, error) {
	p.calls++
	if p.before != nil {
		p.before()
	}
	return p.result, p.err
}

func pipeline(t *testing.T, datasourceEnabled, provisionerEnabled bool) (*Engine, *state.MemoryStore, *countingDatasource, *countingProvisioner) {
	t.Helper()
	store := state.NewMemoryStore()
	datasource := &countingDatasource{metadata: plugin.Metadata{InstanceID: "vm-1", LocalHostname: "worker-1"}}
	provisioner := &countingProvisioner{}
	catalog := plugin.Catalog{Datasources: []plugin.DatasourceRegistration{{Enabled: datasourceEnabled, Plugin: datasource}}, Provisioners: []plugin.ProvisionerRegistration{{Enabled: provisionerEnabled, Plugin: provisioner}}}
	engine := NewWithPlugins(store, slog.New(slog.NewJSONHandler(io.Discard, nil)), catalog, "test-config")
	return engine, store, datasource, provisioner
}

func TestDisabledPluginsNeverExecuteEvenWithPendingCheckpoint(t *testing.T) {
	t.Parallel()
	engine, store, datasource, provisioner := pipeline(t, false, false)
	current := state.Initial(time.Now())
	current.Provisioning = "reboot_required"
	current.Plugins = map[string]state.PluginState{"hostname": {Status: "reboot_required", RebootRequired: true}}
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(context.Background())
	if err != nil || got.Provisioning != "disabled" || got.Plugins["hostname"].Status != "disabled" {
		t.Fatalf("state = %#v, %v", got, err)
	}
	if datasource.calls != 0 || provisioner.calls != 0 {
		t.Fatalf("disabled plugins executed: datasource=%d provisioner=%d", datasource.calls, provisioner.calls)
	}
}

func TestDisablingOnlyProvisionerStillAllowsMetadata(t *testing.T) {
	t.Parallel()
	engine, _, datasource, provisioner := pipeline(t, true, false)
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if datasource.calls != 1 || provisioner.calls != 0 || current.Provisioning != "metadata_available" {
		t.Fatalf("calls=%d/%d state=%#v", datasource.calls, provisioner.calls, current)
	}
}

func TestDisabledDatasourceBlocksProvisioners(t *testing.T) {
	t.Parallel()
	engine, _, datasource, provisioner := pipeline(t, false, true)
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if datasource.calls != 0 || provisioner.calls != 0 || current.Provisioning != "no_datasource" {
		t.Fatalf("calls=%d/%d state=%#v", datasource.calls, provisioner.calls, current)
	}
}

func TestRebootCheckpointPersistsBeforeMutationAndResumes(t *testing.T) {
	t.Parallel()
	engine, store, _, provisioner := pipeline(t, true, true)
	provisioner.result = plugin.Result{Changed: true, RebootRequired: true}
	provisioner.before = func() {
		checkpoint, err := store.Load(context.Background())
		if err != nil || checkpoint.Plugins["hostname"].Status != "running" || checkpoint.Attempts["hostname"] != 1 || checkpoint.Provisioning != "running" {
			t.Fatalf("missing pre-mutation checkpoint: %#v %v", checkpoint, err)
		}
	}
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	pending, err := store.Load(context.Background())
	if err != nil || pending.Provisioning != "reboot_required" || !pending.Plugins["hostname"].RebootRequired {
		t.Fatalf("pending = %#v, %v", pending, err)
	}
	// On the next boot the plugin observes the active platform state and confirms
	// the operation; lifecycle must not blindly skip the pending operation.
	provisioner.before = nil
	provisioner.result = plugin.Result{}
	if err := engine.reconcile(context.Background(), &pending); err != nil {
		t.Fatal(err)
	}
	if pending.Provisioning != "completed" || pending.Plugins["hostname"].RebootRequired || provisioner.calls != 2 {
		t.Fatalf("resumed state = %#v calls=%d", pending, provisioner.calls)
	}
}

func TestPluginFailureIsDurableAndSanitized(t *testing.T) {
	t.Parallel()
	engine, store, _, provisioner := pipeline(t, true, true)
	want := errors.New("sensitive untrusted plugin output")
	provisioner.err = want
	current := state.Initial(time.Now())
	err := engine.reconcile(context.Background(), &current)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	failed, loadErr := store.Load(context.Background())
	if loadErr != nil || failed.Provisioning != "failed" || failed.LastError == nil || failed.LastError.Message == want.Error() {
		t.Fatalf("failed = %#v %v", failed, loadErr)
	}
}

func TestNoMutationWhenCheckpointCannotBeSaved(t *testing.T) {
	t.Parallel()
	engine, store, datasource, provisioner := pipeline(t, true, true)
	engine.store = rejectMutationCheckpoint{MemoryStore: store}
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err == nil {
		t.Fatal("missing state save error")
	}
	if provisioner.calls != 0 || datasource.calls != 1 {
		t.Fatal("provisioner ran without durable checkpoint")
	}
}

func TestUnavailableDatasourceNeverReportsCompleted(t *testing.T) {
	t.Parallel()
	engine, _, datasource, provisioner := pipeline(t, true, true)
	datasource.err = plugin.ErrUnavailable
	current := state.Initial(time.Now())
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Provisioning != "no_datasource" || provisioner.calls != 0 {
		t.Fatalf("state=%#v calls=%d", current, provisioner.calls)
	}
}

func TestFreshDiscoveryDoesNotReuseProvisionerResult(t *testing.T) {
	t.Parallel()
	for _, previousStatus := range []string{"completed", "reboot_required"} {
		for _, scenario := range []string{"unavailable", "malformed", "ambiguous", "disabled"} {
			t.Run(previousStatus+"/"+scenario, func(t *testing.T) {
				engine, store, noCloud, provisioner := pipeline(t, true, true)
				provisioner.result = plugin.Result{RebootRequired: previousStatus == "reboot_required"}
				current := state.Initial(time.Now())
				if err := engine.reconcile(context.Background(), &current); err != nil {
					t.Fatal(err)
				}
				if current.Plugins["hostname"].Status != previousStatus {
					t.Fatalf("missing prior result: %#v", current)
				}
				provisioner.calls = 0
				engine.configurationHash = "changed-datasources"
				configDrive := &configDriveCounter{countingDatasource: countingDatasource{metadata: noCloud.metadata}}
				engine.catalog.Datasources = append(engine.catalog.Datasources, plugin.DatasourceRegistration{Enabled: true, Plugin: configDrive})
				wantProvisioning := "failed"
				switch scenario {
				case "unavailable":
					noCloud.err, configDrive.err = plugin.ErrUnavailable, plugin.ErrUnavailable
					wantProvisioning = "no_datasource"
				case "malformed":
					configDrive.err = errors.New("malformed ConfigDrive metadata")
				case "ambiguous":
					// Both sources are available, even with matching instance IDs.
				case "disabled":
					for index := range engine.catalog.Datasources {
						engine.catalog.Datasources[index].Enabled = false
					}
					wantProvisioning = "no_datasource"
				}
				noCloud.before = func() {
					saved, err := store.Load(context.Background())
					if err != nil || saved.Plugins["hostname"] != (state.PluginState{Status: "pending"}) {
						t.Fatalf("discovery reused prior hostname result: %#v %v", saved, err)
					}
				}
				err := engine.reconcile(context.Background(), &current)
				if (err != nil) != (wantProvisioning == "failed") {
					t.Fatalf("reconcile() error=%v, want provisioning=%s", err, wantProvisioning)
				}
				saved, err := store.Load(context.Background())
				if err != nil || saved.Provisioning != wantProvisioning || saved.ConfigurationHash != engine.configurationHash || saved.Plugins["hostname"] != (state.PluginState{Status: "pending"}) || saved.Checkpoint != "" {
					t.Fatalf("stale provisioner result persisted: %#v %v", saved, err)
				}
				if provisioner.calls != 0 || saved.Attempts["hostname"] != 1 {
					t.Fatalf("provisioner ran without a selected source: calls=%d attempts=%d", provisioner.calls, saved.Attempts["hostname"])
				}
			})
		}
	}
}
