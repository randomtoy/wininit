package state

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSchemaTwoMigrationPreservesOldEvidenceWithoutInventingExecutions(t *testing.T) {
	t.Parallel()
	old := Initial(time.Now())
	old.SchemaVersion = 2
	old.InstanceID = "legacy-instance"
	old.Provisioning = "completed"
	old.Plugins = map[string]PluginState{"hostname": {Status: "completed"}}
	data := rawCheckpoint(t, old)
	migrated, err := decodeCheckpoint(data)
	if err != nil || migrated.SchemaVersion != CurrentSchemaVersion || migrated.Plugins["hostname"].Status != "completed" || len(migrated.Executions) != 0 {
		t.Fatalf("migration=%#v error=%v", migrated, err)
	}
}
func TestExecutionJournalValidationAndMemoryIsolation(t *testing.T) {
	t.Parallel()
	now := time.Now()
	key := strings.Repeat("a", 64)
	record := Execution{ApprovalHash: strings.Repeat("b", 64), BootID: strings.Repeat("c", 64), Status: "started", StartedAt: now}
	value := Initial(now)
	value.Executions = map[string]Execution{key: record}
	store := NewMemoryStore()
	if err := store.Save(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	changed := record
	changed.Status = "succeeded"
	changed.HasExitCode = true
	changed.FinishedAt = now
	value.Executions[key] = changed
	loaded, err := store.Load(context.Background())
	if err != nil || loaded.Executions[key].Status != "started" {
		t.Fatal("execution map was aliased during Save")
	}
	loaded.Executions[key] = changed
	loaded, err = store.Load(context.Background())
	if err != nil || loaded.Executions[key].Status != "started" {
		t.Fatal("execution map was aliased during Load")
	}
	for _, modify := range []func(*Execution){func(e *Execution) { e.BootID = "invalid" }, func(e *Execution) { e.Status = "invented" }, func(e *Execution) { e.HasExitCode = true }, func(e *Execution) { e.FailureCode = "raw-fixture-private-error" }} {
		invalid := record
		modify(&invalid)
		value.Executions[key] = invalid
		if value.Validate() == nil {
			t.Fatal("invalid execution evidence accepted")
		}
	}
}
