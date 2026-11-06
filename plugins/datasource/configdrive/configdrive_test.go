package configdrive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomtoy/wininit/internal/plugin"
)

func TestParseMetadataAndHostnameNormalization(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ input, hostname string }{
		{`{"uuid":"vm-001","hostname":"Worker-01.example.invalid","meta":{"password":"fixture-secret"}}`, "worker-01"},
		{`{"uuid":"vm-001","hostname":"WORKER-01."}`, "worker-01"},
		{`{"uuid":"vm-001","hostname":"worker-01"}`, "worker-01"},
		{`{"uuid":"vm-001","name":"display name must not be a hostname"}`, ""},
		{`{"uuid":"vm-001","hostname":""}`, ""},
		{`{"uuid":"vm-001","hostname":"abcdefghijklmnop.example.invalid"}`, "abcdefghijklmnop"},
	} {
		got, err := Parse([]byte(tc.input))
		if err != nil || got.InstanceID != "vm-001" || got.LocalHostname != tc.hostname {
			t.Fatalf("Parse()=%#v, %v", got, err)
		}
	}
}

func TestRejectMalformedMetadataWithoutEchoingInput(t *testing.T) {
	t.Parallel()
	for name, input := range map[string]string{
		"missing uuid":      `{"hostname":"worker"}`,
		"null uuid":         `{"uuid":null}`,
		"numeric uuid":      `{"uuid":12}`,
		"empty uuid":        `{"uuid":""}`,
		"whitespace uuid":   `{"uuid":" vm"}`,
		"control uuid":      `{"uuid":"vm\nprivate"}`,
		"surrogate uuid":    `{"uuid":"\ud800"}`,
		"long uuid":         `{"uuid":"` + strings.Repeat("x", 257) + `"}`,
		"duplicate":         `{"uuid":"one","uuid":"two"}`,
		"escaped duplicate": `{"uuid":"one","\u0075uid":"two"}`,
		"nested duplicate":  `{"uuid":"vm","meta":{"password":"one","password":"two"}}`,
		"array":             `[{"uuid":"vm"}]`,
		"null":              `null`,
		"trailing":          `{"uuid":"vm"} {"uuid":"other"}`,
		"truncated":         `{"uuid":"vm","password":"fixture-secret`,
		"null hostname":     `{"uuid":"vm","hostname":null}`,
		"numeric hostname":  `{"uuid":"vm","hostname":123}`,
		"invalid hostname":  `{"uuid":"vm","hostname":"fixture-secret/bad"}`,
		"unicode hostname":  `{"uuid":"vm","hostname":"nøde.example"}`,
		"empty label":       `{"uuid":"vm","hostname":"node..example"}`,
		"bad label":         `{"uuid":"vm","hostname":"node.-example"}`,
		"long label":        `{"uuid":"vm","hostname":"` + strings.Repeat("x", 64) + `"}`,
		"long fqdn":         `{"uuid":"vm","hostname":"` + strings.Repeat("node.", 51) + `com"}`,
		"deep JSON":         `{"uuid":"vm","meta":` + strings.Repeat("[", 34) + "0" + strings.Repeat("]", 34) + `}`,
		"invalid utf8":      string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}),
		"size":              strings.Repeat("x", MaxMetadataBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(input))
			if err == nil {
				t.Fatal("malformed metadata accepted")
			}
			if strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("metadata leaked in an error")
			}
		})
	}
}

func TestHighestSupportedVersionWinsAndIgnoresOtherFiles(t *testing.T) {
	t.Parallel()
	seed := t.TempDir()
	writeMetadata(t, seed, "2012-08-10", `{"uuid":"old"}`)
	writeMetadata(t, seed, "2025-04-04", `{"uuid":"new","hostname":"WORKER.cloud.invalid"}`)
	writeMetadata(t, seed, "latest", `{"uuid":"latest-must-not-win"}`)
	for _, name := range []string{"user_data", "password", "vendor_data.json", "network_data.json"} {
		// A directory would fail if the datasource tried reading it as a file.
		if err := os.Mkdir(filepath.Join(seed, "openstack", "2025-04-04", name), 0o000); err != nil {
			t.Fatal(err)
		}
	}
	finder := func(context.Context) ([]string, error) { t.Fatal("explicit seed enumerated drives"); return nil, nil }
	got, err := New(seed, true, finder).Discover(context.Background())
	if err != nil || got.InstanceID != "new" || got.LocalHostname != "worker" {
		t.Fatalf("Discover()=%#v %v", got, err)
	}
}

func TestLatestAndOlderVersionCompatibility(t *testing.T) {
	t.Parallel()
	for _, version := range []string{"latest", "2012-08-10", "2013-04-04", "2013-10-17", "2015-10-15", "2016-06-30", "2016-10-06", "2017-02-22", "2018-08-27", "2020-10-14", "2025-04-04"} {
		t.Run(version, func(t *testing.T) {
			seed := t.TempDir()
			writeMetadata(t, seed, version, `{"uuid":"vm"}`)
			got, err := New(seed, false, nil).Discover(context.Background())
			if err != nil || got.InstanceID != "vm" {
				t.Fatalf("Discover()=%#v %v", got, err)
			}
		})
	}
}

func TestMalformedSelectedVersionDoesNotDowngrade(t *testing.T) {
	t.Parallel()
	for _, malformation := range []string{"missing", "json", "oversize", "not_regular"} {
		t.Run(malformation, func(t *testing.T) {
			seed := t.TempDir()
			writeMetadata(t, seed, "2012-08-10", `{"uuid":"old"}`)
			path := writeMetadata(t, seed, "2025-04-04", `{"uuid":"new"}`)
			switch malformation {
			case "missing":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "json":
				if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				if err := os.WriteFile(path, []byte(strings.Repeat("x", MaxMetadataBytes+1)), 0o600); err != nil {
					t.Fatal(err)
				}
			case "not_regular":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := New(seed, false, nil).Discover(context.Background()); err == nil || errors.Is(err, plugin.ErrUnavailable) {
				t.Fatalf("invalid selected version silently downgraded: %v", err)
			}
		})
	}
}

func TestVolumeDiscoveryAndCancellation(t *testing.T) {
	t.Parallel()
	seed := t.TempDir()
	writeMetadata(t, seed, "latest", `{"uuid":"from-volume"}`)
	for _, tc := range []struct {
		name  string
		roots []string
		err   error
		want  string
	}{
		{"single", []string{seed}, nil, "from-volume"},
		{"missing", nil, nil, "unavailable"},
		{"ambiguous", []string{seed, seed}, nil, "error"},
		{"enumeration error", nil, errors.New("enumeration failed"), "error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New("", true, func(context.Context) ([]string, error) { return tc.roots, tc.err }).Discover(context.Background())
			switch tc.want {
			case "unavailable":
				if !errors.Is(err, plugin.ErrUnavailable) {
					t.Fatalf("error=%v", err)
				}
			case "error":
				if err == nil || errors.Is(err, plugin.ErrUnavailable) {
					t.Fatalf("error=%v", err)
				}
			default:
				if err != nil || got.InstanceID != tc.want {
					t.Fatalf("got=%#v error=%v", got, err)
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New("", true, func(context.Context) ([]string, error) { t.Fatal("canceled discovery performed IO"); return nil, nil }).Discover(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v", err)
	}
	if _, err := New("", false, nil).Discover(context.Background()); !errors.Is(err, plugin.ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
}

func TestUnavailableLayoutsAndSanitizedFileErrors(t *testing.T) {
	t.Parallel()
	seed := t.TempDir()
	if _, err := New(seed, false, nil).Discover(context.Background()); !errors.Is(err, plugin.ErrUnavailable) {
		t.Fatalf("error=%v", err)
	}
	writeMetadata(t, seed, "2099-01-01", `{"uuid":"future-only"}`)
	if _, err := New(seed, false, nil).Discover(context.Background()); !errors.Is(err, plugin.ErrUnavailable) {
		t.Fatalf("future version was assumed compatible: %v", err)
	}
	if _, err := New(filepath.Join(seed, "private-path-must-not-leak"), false, nil).Discover(context.Background()); err == nil || strings.Contains(err.Error(), "private-path") {
		t.Fatalf("file error=%v", err)
	}
}

func TestRejectSymlinkPathComponents(t *testing.T) {
	t.Parallel()
	for _, component := range []string{"openstack", "latest", "meta_data.json"} {
		t.Run(component, func(t *testing.T) {
			seed := t.TempDir()
			path := writeMetadata(t, seed, "latest", `{"uuid":"inside"}`)
			targetSeed := t.TempDir()
			target := writeMetadata(t, targetSeed, "latest", `{"uuid":"outside"}`)
			switch component {
			case "openstack":
				path = filepath.Join(seed, "openstack")
				target = filepath.Join(targetSeed, "openstack")
			case "latest":
				path = filepath.Join(seed, "openstack", "latest")
				target = filepath.Join(targetSeed, "openstack", "latest")
			}
			// Only the exact test-created file/directory is removed and replaced.
			if component != "meta_data.json" {
				if err := os.Remove(filepath.Join(seed, "openstack", "latest", "meta_data.json")); err != nil {
					t.Fatal(err)
				}
			}
			if component == "openstack" {
				if err := os.Remove(filepath.Join(seed, "openstack", "latest")); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlink privilege unavailable: %v", err)
			}
			if _, err := New(seed, false, nil).Discover(context.Background()); err == nil {
				t.Fatal("symlink metadata path accepted")
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	f.Add([]byte(`{"uuid":"vm","hostname":"worker.example.invalid"}`))
	f.Add([]byte(`{"uuid":null}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		metadata, err := Parse(data)
		if err == nil {
			if metadata.InstanceID == "" || len(metadata.InstanceID) > 256 || strings.Contains(metadata.LocalHostname, ".") {
				t.Fatalf("invalid normalized metadata: %#v", metadata)
			}
		}
	})
}

func writeMetadata(t *testing.T, seed, version, content string) string {
	t.Helper()
	directory := filepath.Join(seed, "openstack", version)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "meta_data.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
