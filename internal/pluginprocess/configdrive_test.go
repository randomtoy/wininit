package pluginprocess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/randomtoy/wininit/internal/plugin"
)

var fixtureBinary, configDriveBinary string

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "wininit-plugin-test-")
	if err != nil {
		panic(err)
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	fixtureBinary, configDriveBinary = filepath.Join(directory, "fixture"+suffix), filepath.Join(directory, "configdrive"+suffix)
	for _, build := range []struct{ path, source string }{{fixtureBinary, "./tests/fixtures/pluginprocess"}, {configDriveBinary, "./cmd/wininit-plugin-configdrive"}} {
		cmd := exec.Command("go", "build", "-o", build.path, build.source)
		cmd.Dir = filepath.Join("..", "..")
		if data, err := cmd.CombinedOutput(); err != nil {
			os.RemoveAll(directory)
			panic(string(data))
		}
	}
	code := m.Run()
	os.RemoveAll(directory)
	os.Exit(code)
}
func processSettings(t *testing.T, mode string) Settings {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, mode)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	source := fixtureBinary
	if mode == "configdrive" {
		source = configDriveBinary
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	prepareTestPath(t, directory)
	digest := sha256.Sum256(data)
	return Settings{Executable: path, SHA256: hex.EncodeToString(digest[:]), TimeoutSeconds: 15}
}
func TestExternalConfigDriveReadsRealFixture(t *testing.T) {
	t.Parallel()
	settings := processSettings(t, "configdrive")
	seed := t.TempDir()
	path := filepath.Join(seed, "openstack", "latest")
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "meta_data.json"), []byte(`{"uuid":"external-instance","hostname":"EXTERNAL.example.invalid"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prepareTestPath(t, seed)
	metadata, err := NewConfigDrive(settings, seed, false).Discover(context.Background())
	if err != nil || metadata.InstanceID != "external-instance" || metadata.LocalHostname != "external" {
		t.Fatalf("Discover()=%#v %v", metadata, err)
	}
}
func TestExternalFailuresAreBoundedAndSanitized(t *testing.T) {
	for _, scenario := range []struct{ mode, message string }{
		{"version", "handshake"}, {"identity", "identity"}, {"crash", "discovery"},
		{"leak", "discovery"}, {"invalid", "uuid"}, {"unnormalized", "not normalized"},
		{"conflict", "conflicting availability"}, {"hang", "deadline exceeded"},
		{"nohandshake", "handshake"}, {"silent", "deadline exceeded"},
	} {
		mode := scenario.mode
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			settings := processSettings(t, mode)
			if mode == "hang" || mode == "silent" {
				settings.TimeoutSeconds = 3
			}
			start := time.Now()
			_, err := NewConfigDrive(settings, "", false).Discover(context.Background())
			if err == nil || errors.Is(err, plugin.ErrUnavailable) || strings.Contains(err.Error(), "fixture-private-secret") {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(err.Error(), scenario.message) {
				t.Fatalf("%s failed at the wrong boundary: %v", mode, err)
			}
			if time.Since(start) > time.Duration(settings.TimeoutSeconds+6)*time.Second {
				t.Fatal("process deadline/cleanup was not bounded")
			}
		})
	}
}
func TestUnavailableAndEnvironmentIsolation(t *testing.T) {
	t.Setenv("WININIT_TEST_SECRET", "fixture-private-secret")
	for _, mode := range []string{"unavailable", "environment"} {
		settings := processSettings(t, mode)
		got, err := NewConfigDrive(settings, "", false).Discover(context.Background())
		if mode == "unavailable" && !errors.Is(err, plugin.ErrUnavailable) {
			t.Fatalf("unavailable error=%v", err)
		}
		if mode == "environment" && (err != nil || got.InstanceID == "") {
			t.Fatalf("environment isolation failed: %#v %v", got, err)
		}
	}
}
func TestChecksumAndDisabledConstructionNeverLaunch(t *testing.T) {
	t.Parallel()
	settings := processSettings(t, "environment")
	settings.SHA256 = strings.Repeat("0", 64)
	if _, err := NewConfigDrive(settings, "", false).Discover(context.Background()); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("checksum error=%v", err)
	}
	// Construction and pre-cancelled execution cannot touch an absent image.
	settings.Executable = filepath.Join(t.TempDir(), "missing")
	_ = NewConfigDrive(settings, "", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewConfigDrive(settings, "", false).Discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error=%v", err)
	}
}
func TestCancellationStopsRunningPlugin(t *testing.T) {
	t.Parallel()
	settings := processSettings(t, "hang")
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := NewConfigDrive(settings, "", false).Discover(ctx)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 8*time.Second {
		t.Fatalf("cancel error=%v after %s", err, time.Since(start))
	}
}
