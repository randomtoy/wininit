package nocloud

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomtoy/wininit/internal/plugin"
)

func TestParse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, input string
		valid       bool
	}{
		{"valid", "instance-id: vm-1\nlocal-hostname: worker-1\n", true},
		{"extensions", "instance-id: vm-1\npublic-keys: [ignored]\n", true},
		{"missing instance", "local-hostname: worker-1", false},
		{"numeric instance", "instance-id: 12", false},
		{"duplicate", "instance-id: one\ninstance-id: two", false},
		{"multi doc", "instance-id: one\n---\ninstance-id: two", false},
		{"alias", "instance-id: &id one\nlocal-hostname: *id", false},
		{"control", `instance-id: "one\nsecret"`, false},
		{"wrong root", "- list", false},
		{"invalid YAML", "secret: [password", false},
		{"size", strings.Repeat("x", MaxMetadataBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metadata, err := Parse([]byte(tc.input))
			if (err == nil) != tc.valid {
				t.Fatalf("Parse() = %#v, %v", metadata, err)
			}
			if err != nil && strings.Contains(err.Error(), "password") {
				t.Fatal("parser leaked untrusted input")
			}
		})
	}
}

func TestExplicitSeedDoesNotDiscoverOrReadUserData(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "meta-data"), []byte("instance-id: vm-1\nlocal-hostname: worker-1"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A directory is not readable as user-data; discovery must never open it.
	if err := os.Mkdir(filepath.Join(directory, "user-data"), 0o000); err != nil {
		t.Fatal(err)
	}
	p := New(directory, true, func(context.Context) ([]string, error) { t.Fatal("explicit seed scanned volumes"); return nil, nil })
	got, err := p.Discover(context.Background())
	if err != nil || got.InstanceID != "vm-1" || got.LocalHostname != "worker-1" {
		t.Fatalf("Discover() = %#v, %v", got, err)
	}
}

func TestDiscoveryUnavailableAmbiguousAndCancellation(t *testing.T) {
	t.Parallel()
	if _, err := New("", true, func(context.Context) ([]string, error) { return nil, nil }).Discover(context.Background()); !errors.Is(err, plugin.ErrUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if _, err := New("", true, func(context.Context) ([]string, error) { return []string{"one", "two"}, nil }).Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "multiple CIDATA") {
		t.Fatalf("error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New("", true, nil).Discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestRejectSymlinkMetadata(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	out := filepath.Join(t.TempDir(), "meta-data")
	if err := os.WriteFile(out, []byte("instance-id: outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(directory, "meta-data")); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if _, err := New(directory, false, nil).Discover(context.Background()); err == nil {
		t.Fatal("symlink meta-data accepted")
	}
}
