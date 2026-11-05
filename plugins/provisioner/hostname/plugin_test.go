package hostname

import (
	"context"
	"errors"
	"strings"
	"testing"

	pluginapi "github.com/randomtoy/wininit/internal/plugin"
)

type fakePlatform struct {
	active       string
	configured   string
	snapshotErr  error
	setErr       error
	setCalls     int
	setHostname  string
	snapshotCall int
}

func (f *fakePlatform) Snapshot(context.Context) (string, string, error) {
	f.snapshotCall++
	return f.active, f.configured, f.snapshotErr
}

func (f *fakePlatform) Set(_ context.Context, hostname string) error {
	f.setCalls++
	f.setHostname = hostname
	if f.setErr == nil {
		f.configured = hostname
	}
	return f.setErr
}

func TestApplyTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		active          string
		configured      string
		want            pluginapi.Result
		wantSetCalls    int
		wantSetHostname string
	}{
		{
			name:       "already active and configured",
			active:     "WINNODE",
			configured: "winnode",
		},
		{
			name:       "configured and awaiting reboot",
			active:     "OLDNODE",
			configured: "WINNODE",
			want:       pluginapi.Result{RebootRequired: true},
		},
		{
			name:            "active name has stale configuration",
			active:          "WINNODE",
			configured:      "OLDNODE",
			want:            pluginapi.Result{Changed: true, RebootRequired: true},
			wantSetCalls:    1,
			wantSetHostname: "WINNODE",
		},
		{
			name:            "new name must be configured",
			active:          "OLDNODE",
			configured:      "OLDNODE",
			want:            pluginapi.Result{Changed: true, RebootRequired: true},
			wantSetCalls:    1,
			wantSetHostname: "WINNODE",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			platform := &fakePlatform{active: test.active, configured: test.configured}
			got, err := New(platform).Apply(context.Background(), pluginapi.Metadata{LocalHostname: "WINNODE"})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Apply() = %+v, want %+v", got, test.want)
			}
			if platform.setCalls != test.wantSetCalls {
				t.Errorf("Set() calls = %d, want %d", platform.setCalls, test.wantSetCalls)
			}
			if platform.setHostname != test.wantSetHostname {
				t.Errorf("Set() hostname = %q, want %q", platform.setHostname, test.wantSetHostname)
			}
		})
	}
}

func TestApplyMissingHostnameIsNoOp(t *testing.T) {
	t.Parallel()

	platform := &fakePlatform{snapshotErr: errors.New("must not be called")}
	got, err := New(platform).Apply(context.Background(), pluginapi.Metadata{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if got != (pluginapi.Result{Skipped: true}) {
		t.Fatalf("Apply() = %+v, want skipped result", got)
	}
	if platform.snapshotCall != 0 {
		t.Fatalf("Snapshot() calls = %d, want 0", platform.snapshotCall)
	}
}

func TestApplyRepeatedRunSetsOnceThenWaitsForReboot(t *testing.T) {
	t.Parallel()

	platform := &fakePlatform{active: "OLDNODE", configured: "OLDNODE"}
	provisioner := New(platform)
	metadata := pluginapi.Metadata{LocalHostname: "WINNODE"}

	first, err := provisioner.Apply(context.Background(), metadata)
	if err != nil {
		t.Fatalf("first Apply() error = %v", err)
	}
	if first != (pluginapi.Result{Changed: true, RebootRequired: true}) {
		t.Fatalf("first Apply() = %+v, want changed with reboot", first)
	}

	second, err := provisioner.Apply(context.Background(), metadata)
	if err != nil {
		t.Fatalf("second Apply() error = %v", err)
	}
	if second != (pluginapi.Result{RebootRequired: true}) {
		t.Fatalf("second Apply() = %+v, want pending reboot", second)
	}
	if platform.setCalls != 1 {
		t.Fatalf("Set() calls before reboot = %d, want 1", platform.setCalls)
	}

	platform.active = "WINNODE"
	third, err := provisioner.Apply(context.Background(), metadata)
	if err != nil {
		t.Fatalf("third Apply() error = %v", err)
	}
	if third != (pluginapi.Result{}) {
		t.Fatalf("third Apply() = %+v, want converged no-op", third)
	}
	if platform.setCalls != 1 {
		t.Fatalf("Set() calls after simulated reboot = %d, want 1", platform.setCalls)
	}
}

func TestApplyRejectsInvalidHostnameBeforePlatformAccess(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"-node",
		"node-",
		"12345",
		"node.example",
		"node_name",
		"abcdefghijklmnop",
		"nøde",
		" node",
	}
	for _, hostname := range invalid {
		hostname := hostname
		t.Run(hostname, func(t *testing.T) {
			t.Parallel()
			platform := &fakePlatform{}
			_, err := New(platform).Apply(context.Background(), pluginapi.Metadata{LocalHostname: hostname})
			if err == nil {
				t.Fatal("Apply() error = nil, want validation error")
			}
			if strings.Contains(err.Error(), hostname) {
				t.Fatalf("Apply() error %q contains untrusted hostname", err)
			}
			if platform.snapshotCall != 0 || platform.setCalls != 0 {
				t.Fatalf("platform accessed for invalid hostname: snapshot=%d set=%d", platform.snapshotCall, platform.setCalls)
			}
		})
	}
}

func TestApplyAcceptsValidHostnameForms(t *testing.T) {
	t.Parallel()

	valid := []string{
		"a",
		"A1",
		"node-01",
		"abcdefghijklmno",
	}
	for _, hostname := range valid {
		hostname := hostname
		t.Run(hostname, func(t *testing.T) {
			t.Parallel()
			platform := &fakePlatform{active: hostname, configured: hostname}
			got, err := New(platform).Apply(context.Background(), pluginapi.Metadata{LocalHostname: hostname})
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if got != (pluginapi.Result{}) {
				t.Fatalf("Apply() = %+v, want no-op result", got)
			}
		})
	}
}

func TestApplyRequiresPlatformWhenHostnamePresent(t *testing.T) {
	t.Parallel()

	_, err := New(nil).Apply(context.Background(), pluginapi.Metadata{LocalHostname: "WINNODE"})
	if err == nil {
		t.Fatal("Apply() error = nil, want missing platform error")
	}
}

func TestApplyPropagatesErrorsWithoutClaimingChange(t *testing.T) {
	t.Parallel()

	t.Run("snapshot", func(t *testing.T) {
		t.Parallel()
		wantErr := errors.New("snapshot failed")
		platform := &fakePlatform{snapshotErr: wantErr}
		got, err := New(platform).Apply(context.Background(), pluginapi.Metadata{LocalHostname: "WINNODE"})
		if !errors.Is(err, wantErr) {
			t.Fatalf("Apply() error = %v, want wrapped %v", err, wantErr)
		}
		if got != (pluginapi.Result{}) {
			t.Fatalf("Apply() = %+v, want empty result", got)
		}
	})

	t.Run("set", func(t *testing.T) {
		t.Parallel()
		wantErr := errors.New("set failed")
		platform := &fakePlatform{active: "OLD", configured: "OLD", setErr: wantErr}
		got, err := New(platform).Apply(context.Background(), pluginapi.Metadata{LocalHostname: "WINNODE"})
		if !errors.Is(err, wantErr) {
			t.Fatalf("Apply() error = %v, want wrapped %v", err, wantErr)
		}
		if got != (pluginapi.Result{}) {
			t.Fatalf("Apply() = %+v, want empty result", got)
		}
	})
}

func TestApplyHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	platform := &fakePlatform{}
	_, err := New(platform).Apply(ctx, pluginapi.Metadata{LocalHostname: "WINNODE"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply() error = %v, want context.Canceled", err)
	}
	if platform.snapshotCall != 0 || platform.setCalls != 0 {
		t.Fatal("platform accessed with canceled context")
	}
}

func TestName(t *testing.T) {
	t.Parallel()

	if got := New(&fakePlatform{}).Name(); got != "hostname" {
		t.Fatalf("Name() = %q, want hostname", got)
	}
}
