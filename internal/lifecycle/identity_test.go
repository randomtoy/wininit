package lifecycle

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/pluginprocess"
	"github.com/randomtoy/wininit/internal/state"
	"github.com/randomtoy/wininit/plugins/provisioner/authorizedkeys"
	"github.com/randomtoy/wininit/plugins/provisioner/localusers"
	"github.com/randomtoy/wininit/plugins/provisioner/passwordpolicy"
	"golang.org/x/crypto/ssh"
)

type identityTestPlatform struct {
	t                                                    *testing.T
	store                                                *state.MemoryStore
	disabled                                             bool
	policy                                               identity.PasswordPolicy
	user                                                 localusers.UserState
	exists                                               bool
	keys                                                 []byte
	order                                                []string
	secretReads, accountCreates, keyWrites, policyWrites int
}

func (p *identityTestPlatform) checkpoint(name string) {
	p.t.Helper()
	if p.disabled {
		p.t.Fatalf("disabled plugin %s accessed its platform", name)
	}
	current, err := p.store.Load(context.Background())
	if err != nil || current.Plugins[name].Status != "running" || current.Provisioning != "running" || current.Attempts[name] < 1 {
		p.t.Fatalf("missing durable %s pre-mutation checkpoint: %#v %v", name, current, err)
	}
	encoded, err := json.Marshal(current)
	if err != nil || bytes.Contains(encoded, []byte("fixture-initial-password")) {
		p.t.Fatal("secret appeared in durable checkpoint")
	}
}
func (p *identityTestPlatform) PasswordPolicy(context.Context) (identity.PasswordPolicy, error) {
	p.checkpoint("password_policy")
	return p.policy, nil
}
func (p *identityTestPlatform) SetPasswordPolicy(_ context.Context, desired identity.PasswordPolicy) error {
	p.checkpoint("password_policy")
	p.order = append(p.order, "password_policy")
	p.policy = desired
	p.policyWrites++
	return nil
}
func (p *identityTestPlatform) User(context.Context, string) (localusers.UserState, bool, error) {
	p.checkpoint("local_users")
	return p.user, p.exists, nil
}
func (p *identityTestPlatform) ReadPassword(context.Context, string) ([]byte, error) {
	p.checkpoint("local_users")
	p.secretReads++
	return []byte("fixture-initial-password"), nil
}
func (p *identityTestPlatform) CreateUser(_ context.Context, user identity.User, _ []byte) error {
	p.checkpoint("local_users")
	p.order = append(p.order, "local_users")
	p.exists = true
	p.user = localusers.UserState{FullName: user.FullName, Description: user.Description, PasswordNeverExpires: user.PasswordNeverExpires}
	p.accountCreates++
	return nil
}
func (p *identityTestPlatform) UpdateUser(context.Context, identity.User) error {
	p.checkpoint("local_users")
	p.t.Fatal("unexpected account update on unchanged input")
	return nil
}
func (p *identityTestPlatform) ReconcileKeys(_ context.Context, _ string, keys []string) (bool, error) {
	p.checkpoint("authorized_keys")
	merged, err := identity.MergeKeys(p.keys, keys)
	if err != nil {
		return false, err
	}
	if bytes.Equal(merged, p.keys) {
		return false, nil
	}
	p.keys = merged
	p.keyWrites++
	p.order = append(p.order, "authorized_keys")
	return true, nil
}
func TestIdentityPipelineCheckpointsOrderRerunAndInputHash(t *testing.T) {
	t.Parallel()
	engine, store, _, _ := pipeline(t, true, false)
	platform := &identityTestPlatform{t: t, store: store}
	user := identity.User{Name: "worker", PasswordFile: filepath.Join(t.TempDir(), "initial-password")}
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	engine.catalog.Provisioners = []plugin.ProvisionerRegistration{
		{Enabled: true, Plugin: passwordpolicy.New(identity.DefaultPasswordPolicy(), platform)},
		{Enabled: true, Plugin: localusers.New([]identity.User{user}, platform)},
		{Enabled: true, Plugin: authorizedkeys.New([]identity.KeyUser{{Name: user.Name, PublicKeys: []string{strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))}}}, platform)},
	}
	current := state.Initial(time.Now())
	for run := 0; run < 2; run++ {
		if err := engine.reconcile(context.Background(), &current); err != nil {
			t.Fatal(err)
		}
		if current.Provisioning != "completed" {
			t.Fatalf("provisioning=%s", current.Provisioning)
		}
	}
	if strings.Join(platform.order, ",") != "password_policy,local_users,authorized_keys" || platform.secretReads != 1 || platform.accountCreates != 1 || platform.policyWrites != 1 || platform.keyWrites != 1 {
		t.Fatalf("non-idempotent identity run: %#v", platform)
	}
	hash := current.Plugins["local_users"].InputHash
	engine.configurationHash = "changed-identity-configuration"
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	if current.Plugins["local_users"].InputHash == hash {
		t.Fatal("configured identity input was absent from checkpoint hash")
	}
}
func TestDisabledIdentityAndProcessPluginsIgnoreOldCheckpoints(t *testing.T) {
	t.Parallel()
	engine, store, _, _ := pipeline(t, true, false)
	platform := &identityTestPlatform{t: t, store: store, disabled: true}
	engine.catalog.Provisioners = []plugin.ProvisionerRegistration{
		{Plugin: passwordpolicy.New(identity.PasswordPolicy{}, platform)},
		{Plugin: localusers.New(nil, platform)},
		{Plugin: authorizedkeys.New(nil, platform)},
	}
	engine.catalog.Datasources = append(engine.catalog.Datasources, plugin.DatasourceRegistration{Plugin: pluginprocess.NewConfigDrive(pluginprocess.Settings{Executable: filepath.Join(t.TempDir(), "missing.exe")}, "", true)})
	current := state.Initial(time.Now())
	current.Plugins = map[string]state.PluginState{}
	for _, name := range []string{"password_policy", "local_users", "authorized_keys", "configdrive"} {
		current.Plugins[name] = state.PluginState{Status: "reboot_required", InputHash: "old-input", RebootRequired: true}
	}
	if err := engine.reconcile(context.Background(), &current); err != nil {
		t.Fatal(err)
	}
	for name, checkpoint := range current.Plugins {
		if name == "nocloud" {
			continue
		}
		if checkpoint != (state.PluginState{Status: "disabled"}) {
			t.Fatalf("%s retained execution intent: %#v", name, checkpoint)
		}
	}
	if current.Provisioning != "metadata_available" {
		t.Fatalf("state=%#v", current)
	}
}
