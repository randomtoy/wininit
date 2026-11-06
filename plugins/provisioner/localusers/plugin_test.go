package localusers

import (
	"context"
	"errors"
	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"path/filepath"
	"testing"
)

type fakePlatform struct {
	state                   UserState
	exists                  bool
	reads, creates, updates int
	password                []byte
	err                     error
}

func (f *fakePlatform) User(context.Context, string) (UserState, bool, error) {
	return f.state, f.exists, f.err
}
func (f *fakePlatform) ReadPassword(context.Context, string) ([]byte, error) {
	f.reads++
	f.password = []byte("fixture-initial-secret")
	return f.password, f.err
}
func (f *fakePlatform) CreateUser(_ context.Context, user identity.User, password []byte) error {
	f.creates++
	if string(password) != "fixture-initial-secret" {
		return errors.New("wrong secret")
	}
	f.exists = true
	f.state = UserState{user.FullName, user.Description, user.PasswordNeverExpires}
	return f.err
}
func (f *fakePlatform) UpdateUser(_ context.Context, user identity.User) error {
	f.updates++
	f.state = UserState{user.FullName, user.Description, user.PasswordNeverExpires}
	return f.err
}
func TestCreateRerunAndAttributeUpdateNeverResetPassword(t *testing.T) {
	t.Parallel()
	desired := identity.User{Name: "worker", PasswordFile: filepath.Join(t.TempDir(), "password")}
	platform := &fakePlatform{}
	p := New([]identity.User{desired}, platform)
	result, err := p.Apply(context.Background(), plugin.Metadata{})
	if err != nil || !result.Changed || platform.creates != 1 || platform.reads != 1 {
		t.Fatalf("create=%#v %v %#v", result, err, platform)
	}
	for _, value := range platform.password {
		if value != 0 {
			t.Fatal("password buffer was retained")
		}
	}
	result, err = p.Apply(context.Background(), plugin.Metadata{})
	if err != nil || result.Changed || platform.reads != 1 || platform.creates != 1 || platform.updates != 0 {
		t.Fatal("rerun reset or reread password")
	}
	p.users[0].Description = "changed"
	result, err = p.Apply(context.Background(), plugin.Metadata{})
	if err != nil || !result.Changed || platform.reads != 1 || platform.updates != 1 {
		t.Fatal("attribute update touched password")
	}
}
func TestFailureAndCancellationDoNotReadSecrets(t *testing.T) {
	t.Parallel()
	desired := identity.User{Name: "worker", PasswordFile: filepath.Join(t.TempDir(), "password")}
	want := errors.New("account query failed")
	platform := &fakePlatform{err: want}
	p := New([]identity.User{desired}, platform)
	if _, err := p.Apply(context.Background(), plugin.Metadata{}); !errors.Is(err, want) || platform.reads != 0 {
		t.Fatal("account failure was bypassed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Apply(ctx, plugin.Metadata{}); !errors.Is(err, context.Canceled) || platform.reads != 0 {
		t.Fatal("cancelled plugin read a secret")
	}
}
