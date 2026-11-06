package passwordpolicy

import (
	"context"
	"errors"
	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"testing"
)

type fakePolicy struct {
	value  identity.PasswordPolicy
	writes int
	err    error
}

func (f *fakePolicy) PasswordPolicy(context.Context) (identity.PasswordPolicy, error) {
	return f.value, f.err
}
func (f *fakePolicy) SetPasswordPolicy(_ context.Context, value identity.PasswordPolicy) error {
	f.writes++
	f.value = value
	return f.err
}
func TestPolicyIsValidatedIdempotentAndFailureVisible(t *testing.T) {
	t.Parallel()
	desired := identity.DefaultPasswordPolicy()
	platform := &fakePolicy{}
	p := New(desired, platform)
	for index := 0; index < 2; index++ {
		result, err := p.Apply(context.Background(), plugin.Metadata{})
		if err != nil || result.Changed != (index == 0) || platform.writes != 1 {
			t.Fatalf("policy=%#v %v writes=%d", result, err, platform.writes)
		}
	}
	platform.err = errors.New("policy access denied")
	if _, err := p.Apply(context.Background(), plugin.Metadata{}); !errors.Is(err, platform.err) {
		t.Fatal("policy read error hidden")
	}
	p.desired.MinimumLength = 0
	if _, err := p.Apply(context.Background(), plugin.Metadata{}); err == nil || platform.writes != 1 {
		t.Fatal("invalid policy mutated platform")
	}
}
