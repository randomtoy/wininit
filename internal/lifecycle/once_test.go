package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/state"
)

type onceFixture struct {
	t                                *testing.T
	store                            state.Store
	id, approval                     string
	prepareCalls, executions, closes int
	fail, skip, reboot               bool
	cancel                           context.CancelFunc
	inspect                          func(string)
}

func (*onceFixture) Name() string { return "powershell_user_data" }
func (p *onceFixture) Apply(context.Context, plugin.Metadata) (plugin.Result, error) {
	p.t.Fatal("one-shot provisioner bypassed the journal")
	return plugin.Result{}, nil
}
func (p *onceFixture) ExecutionIdentity(m plugin.Metadata) (string, string, error) {
	sum := sha256.Sum256([]byte(m.InstanceID + "\x00" + p.id))
	return hex.EncodeToString(sum[:]), p.approval, nil
}
func (p *onceFixture) Prepare(_ context.Context, m plugin.Metadata) (plugin.PreparedExecution, error) {
	p.prepareCalls++
	if m.Datasource != "nocloud" {
		p.t.Fatal("host did not bind selected datasource")
	}
	return plugin.PreparedExecution{Skipped: p.skip, Close: func() { p.closes++ }, Execute: func(ctx context.Context) (plugin.ExecutionResult, error) {
		key, _, _ := p.ExecutionIdentity(m)
		saved, err := p.store.Load(ctx)
		if err != nil || saved.Executions[key].Status != "started" || saved.Provisioning != "running" {
			p.t.Fatalf("missing durable intent before execution: %#v %v", saved, err)
		}
		if p.inspect != nil {
			p.inspect(key)
		}
		p.executions++
		if p.cancel != nil {
			p.cancel()
			return plugin.ExecutionResult{}, ctx.Err()
		}
		if p.fail {
			return plugin.ExecutionResult{HasExitCode: true, ExitCode: 7, FailureCode: "exit_failure"}, errors.New("fixed fixture failure")
		}
		result := plugin.ExecutionResult{Result: plugin.Result{Changed: true}, HasExitCode: true}
		if p.reboot {
			result.ExitCode = 3010
			result.RebootRequired = true
		}
		return result, nil
	}}, nil
}
func onceEngine(t *testing.T) (*Engine, *state.MemoryStore, *onceFixture, state.State) {
	t.Helper()
	engine, store, _, _ := pipeline(t, true, false)
	p := &onceFixture{t: t, store: store, id: "first", approval: strings.Repeat("a", 64)}
	engine.catalog.Provisioners = []plugin.ProvisionerRegistration{{Enabled: true, Plugin: p}}
	return engine, store, p, state.Initial(time.Now())
}
func TestOneShotIntentCacheAndExplicitRetry(t *testing.T) {
	t.Parallel()
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[failure], func(t *testing.T) {
			engine, _, p, current := onceEngine(t)
			p.fail = failure
			err := engine.reconcile(context.Background(), &current)
			if (err != nil) != failure {
				t.Fatal(err)
			}
			engine.configurationHash = "unrelated-configuration-or-image-update"
			err = engine.reconcile(context.Background(), &current)
			if (err != nil) != failure || p.executions != 1 || p.prepareCalls != 1 || p.closes != 1 {
				t.Fatalf("automatic replay: calls=%d prepare=%d close=%d error=%v", p.executions, p.prepareCalls, p.closes, err)
			}
			if !failure && !current.Plugins[p.Name()].Reused {
				t.Fatal("cached completion was reported as fresh execution")
			}
			p.approval = strings.Repeat("b", 64)
			if err := engine.reconcile(context.Background(), &current); err == nil || p.executions != 1 {
				t.Fatal("changing approval silently reused an execution_id")
			}
			p.id = "explicit-retry"
			_ = engine.reconcile(context.Background(), &current)
			if p.executions != 2 {
				t.Fatal("explicit retry was ignored")
			}
		})
	}
}
func TestOneShotUnknownAndCancelledOutcomesNeverReplay(t *testing.T) {
	t.Parallel()
	engine, _, p, current := onceEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	if err := engine.reconcile(ctx, &current); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, record := range current.Executions {
		if record.Status != "interrupted" {
			t.Fatal("cancellation lost its durable outcome")
		}
	}
	p.cancel = nil
	if err := engine.reconcile(context.Background(), &current); err == nil || p.executions != 1 {
		t.Fatal("cancelled execution replayed")
	}
	for key, record := range current.Executions {
		record.Status = "started"
		record.FinishedAt = time.Time{}
		record.FailureCode = ""
		current.Executions[key] = record
	}
	if err := engine.reconcile(context.Background(), &current); err == nil || p.executions != 1 {
		t.Fatal("crashed execution replayed")
	}
	for _, record := range current.Executions {
		if record.Status != "interrupted" || record.FailureCode != "outcome_unknown" {
			t.Fatal("unknown outcome was not retained")
		}
	}
}
func TestOneShotRebootIsNotAServiceRestart(t *testing.T) {
	t.Parallel()
	engine, _, p, current := onceEngine(t)
	p.reboot = true
	boot := strings.Repeat("b", 64)
	engine.bootID = func(context.Context) (string, error) { return boot, nil }
	for i := 0; i < 2; i++ {
		if err := engine.reconcile(context.Background(), &current); err != nil || current.Provisioning != "reboot_required" {
			t.Fatalf("reboot result=%s %v", current.Provisioning, err)
		}
	}
	boot = strings.Repeat("c", 64)
	if err := engine.reconcile(context.Background(), &current); err != nil || current.Provisioning != "completed" || p.executions != 1 {
		t.Fatalf("boot reconciliation replayed script: %s %v", current.Provisioning, err)
	}
}

type intentFailStore struct{ *state.MemoryStore }

func (s intentFailStore) Save(ctx context.Context, value state.State) error {
	for _, record := range value.Executions {
		if record.Status == "started" {
			return errors.New("fixture intent flush failure")
		}
	}
	return s.MemoryStore.Save(ctx, value)
}
func TestOneShotIntentSaveFailurePreventsDispatchAndRetainsNoFalseIntent(t *testing.T) {
	t.Parallel()
	engine, store, p, current := onceEngine(t)
	engine.store = intentFailStore{store}
	if err := engine.reconcile(context.Background(), &current); err == nil || p.executions != 0 || len(current.Executions) != 0 {
		t.Fatal("execution crossed a failed intent flush")
	}
	engine.store = store
	if err := engine.reconcile(context.Background(), &current); err != nil || p.executions != 1 {
		t.Fatalf("known non-dispatch could not recover: %v", err)
	}
}
func TestOneShotDisablementAndAbsentPayloadDoNotConsumeIntent(t *testing.T) {
	t.Parallel()
	engine, _, p, current := onceEngine(t)
	p.skip = true
	if err := engine.reconcile(context.Background(), &current); err != nil || len(current.Executions) != 0 || p.executions != 0 {
		t.Fatal("absent payload consumed intent")
	}
	p.skip = false
	_ = engine.reconcile(context.Background(), &current)
	engine.catalog.Provisioners[0].Enabled = false
	if err := engine.reconcile(context.Background(), &current); err != nil || current.Plugins[p.Name()].Status != "disabled" || p.executions != 1 {
		t.Fatal("disablement executed old intent")
	}
	engine.catalog.Provisioners[0].Enabled = true
	if err := engine.reconcile(context.Background(), &current); err != nil || p.executions != 1 {
		t.Fatal("reenablement replayed a completed script")
	}
}

func TestOneShotRunsAfterEarlierRebootRequestWithRunningAggregate(t *testing.T) {
	engine, _, p, current := onceEngine(t)
	earlier := &countingProvisioner{result: plugin.Result{RebootRequired: true}}
	engine.catalog.Provisioners = append([]plugin.ProvisionerRegistration{{Enabled: true, Plugin: earlier}}, engine.catalog.Provisioners...)
	if err := engine.reconcile(context.Background(), &current); err != nil || p.executions != 1 || current.Provisioning != "reboot_required" {
		t.Fatalf("state=%#v error=%v", current, err)
	}
}

func TestOneShotJournalSurvivesInstanceSwitchAndRefusesEviction(t *testing.T) {
	engine, _, p, current := onceEngine(t)
	source := engine.catalog.Datasources[0].Plugin.(*countingDatasource)
	first := source.metadata.InstanceID
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	source.metadata.InstanceID = "second-instance"
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	source.metadata.InstanceID = first
	if err := engine.reconcile(context.Background(), &current); err != nil || p.executions != 2 {
		t.Fatal("instance switch replayed consumed intent", err)
	}
	var record state.Execution
	for _, item := range current.Executions {
		record = item
		break
	}
	for i := len(current.Executions); i < state.MaxExecutions; i++ {
		key := sha256.Sum256([]byte(time.Unix(int64(i), 0).String()))
		current.Executions[hex.EncodeToString(key[:])] = record
	}
	p.id = "new-work-at-capacity"
	if err := engine.reconcile(context.Background(), &current); err == nil || p.executions != 2 || len(current.Executions) != state.MaxExecutions {
		t.Fatal("full journal dispatched or evicted history")
	}
}

// Two alternating slots model Registry recovery after a damaged active slot.
type recoverySlots struct {
	*state.MemoryStore
	prior state.State
	saves int
}

func (s *recoverySlots) Save(ctx context.Context, value state.State) error {
	if s.saves > 0 {
		s.prior, _ = s.MemoryStore.Load(ctx)
	}
	s.saves++
	return s.MemoryStore.Save(ctx, value)
}
func TestOneShotBothRecoverySlotsContainIntentBeforeDispatch(t *testing.T) {
	engine, store, p, current := onceEngine(t)
	slots := &recoverySlots{MemoryStore: store}
	engine.store = slots
	p.store = slots
	p.inspect = func(key string) {
		if slots.prior.Executions[key].Status != "started" {
			t.Fatal("fallback recovery could forget irreversible intent")
		}
	}
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
}

type cancelAfterFailureSave struct {
	*state.MemoryStore
	cancel context.CancelFunc
}

func (s cancelAfterFailureSave) Save(ctx context.Context, value state.State) error {
	if err := s.MemoryStore.Save(ctx, value); err != nil {
		return err
	}
	if value.Provisioning == "failed" {
		s.cancel()
	}
	return nil
}
func TestConcurrentStopDoesNotErasePrivateExecutionDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine, store, _, p := pipeline(t, true, true)
	p.err = context.DeadlineExceeded // a plugin session deadline, not ctx's cancellation
	engine.store = cancelAfterFailureSave{store, cancel}
	if err := engine.Run(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("plugin deadline was hidden by stop: %v", err)
	}
	saved, err := store.Load(context.Background())
	if err != nil || saved.Provisioning != "failed" || saved.LastError == nil || saved.Phase != state.PhaseFoundationError {
		t.Fatalf("terminal failure was erased: %#v %v", saved, err)
	}
}

func TestConcurrentStopDoesNotEraseJoinedFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine, store, _, p := pipeline(t, true, true)
	failure := errors.New("fixture persistence failure")
	p.before = cancel
	p.err = errors.Join(context.Canceled, failure)
	if err := engine.Run(ctx); !errors.Is(err, failure) {
		t.Fatalf("joined failure hidden by stop: %v", err)
	}
	saved, err := store.Load(context.Background())
	if err != nil || saved.Provisioning != "failed" || saved.LastError == nil {
		t.Fatalf("joined failure lost: %#v %v", saved, err)
	}
}
