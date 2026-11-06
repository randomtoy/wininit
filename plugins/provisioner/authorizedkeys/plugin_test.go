package authorizedkeys

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"golang.org/x/crypto/ssh"
	"strings"
	"testing"
)

type fakeKeys struct {
	calls int
	keys  []string
	err   error
}

func (f *fakeKeys) ReconcileKeys(_ context.Context, _ string, keys []string) (bool, error) {
	f.calls++
	f.keys = keys
	return true, f.err
}
func TestKeysCanonicalizedBeforePlatformAndInvalidBatchNeverRuns(t *testing.T) {
	t.Parallel()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))
	platform := &fakeKeys{}
	p := New([]identity.KeyUser{{Name: "worker", PublicKeys: []string{line + " comment", line}}}, platform)
	result, err := p.Apply(context.Background(), plugin.Metadata{})
	if err != nil || !result.Changed || len(platform.keys) != 1 || platform.keys[0] != line {
		t.Fatalf("keys=%v error=%v", platform.keys, err)
	}
	p.users = append(p.users, identity.KeyUser{Name: "bad/user", PublicKeys: []string{"PRIVATE"}})
	if _, err := p.Apply(context.Background(), plugin.Metadata{}); err == nil || platform.calls != 1 {
		t.Fatal("invalid account batch partially ran")
	}
	p.users = p.users[:1]
	platform.err = errors.New("ACL error")
	if _, err := p.Apply(context.Background(), plugin.Metadata{}); !errors.Is(err, platform.err) {
		t.Fatal("ACL failure hidden")
	}
}
