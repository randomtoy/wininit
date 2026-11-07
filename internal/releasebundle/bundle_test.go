package releasebundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

const testCommit = "0123456789012345678901234567890123456789"

func TestInspectRefusesUnattributableBinaries(t *testing.T) {
	main := "github.com/randomtoy/wininit/cmd/wininit"
	for _, test := range []struct{ key, value string }{
		{"GOOS", "linux"}, {"GOARCH", "arm64"}, {"CGO_ENABLED", "1"},
		{"vcs.revision", strings.Repeat("a", 40)}, {"vcs", ""}, {"vcs.modified", ""},
	} {
		t.Run(test.key, func(t *testing.T) {
			settings := map[string]string{"GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "0", "vcs.revision": testCommit, "vcs": "git", "vcs.modified": "false"}
			valid := &debug.BuildInfo{Path: main, Main: debug.Module{Path: "github.com/randomtoy/wininit"}}
			for key, value := range settings {
				valid.Settings = append(valid.Settings, debug.BuildSetting{Key: key, Value: value})
			}
			if dirty, err := inspect(valid, main, testCommit); err != nil || dirty {
				t.Fatalf("valid binary rejected: dirty=%v, %v", dirty, err)
			}
			for i := range valid.Settings {
				if valid.Settings[i].Key == test.key {
					valid.Settings[i].Value = test.value
				}
			}
			if _, err := inspect(valid, main, testCommit); err == nil {
				t.Fatal("accepted incompatible binary")
			}
		})
	}
}

func TestChecksumSubjectsAreCompleteAndCanonical(t *testing.T) {
	dir := t.TempDir()
	for _, name := range Names() {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, err := checksumContent(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")) != 12 || strings.Contains(string(before), "\r") {
		t.Fatal("checksum set is not twelve LF-terminated subjects")
	}
	if err := os.WriteFile(filepath.Join(dir, "wininit-windows-amd64.msi"), []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := checksumContent(dir)
	if err != nil || string(before) == string(after) {
		t.Fatalf("MSI modification not detected: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, executables[4]+".cdx.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := checksumContent(dir); err == nil {
		t.Fatal("accepted missing plugin SBOM")
	}
}

func TestSBOMRequiresVersionedModuleInventory(t *testing.T) {
	dir := t.TempDir()
	for _, input := range []string{
		`{}`, `{"bomFormat":"SPDX","specVersion":"1.6"}`, `null`,
		`{"bomFormat":"CycloneDX","specVersion":"1.6","metadata":{"component":{"version":"1.0.0"}},"components":[]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, "bom.json"), []byte(input), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := checkSBOM(dir, "bom.json", "1.0.0", &debug.BuildInfo{}); err == nil {
			t.Fatal("accepted missing/invalid module inventory")
		}
	}
}

func TestGenerateDoesNotInventArtifacts(t *testing.T) {
	dir := t.TempDir()
	if err := Generate(dir, "1.2.3", testCommit, "unsigned", true); err == nil {
		t.Fatal("accepted absent distribution")
	}
	if _, err := os.Stat(filepath.Join(dir, ManifestName)); !os.IsNotExist(err) {
		t.Fatal("published manifest for missing distribution")
	}
	for _, version := range []string{"dev", "1.2", "1.2.3-rc.1", "01.2.3", "1.2.3\n"} {
		if err := validateOptions(version, testCommit, "unsigned"); err == nil {
			t.Fatalf("accepted %q", version)
		}
	}
	if err := validateOptions("1.2.3", testCommit, "trusted"); err == nil {
		t.Fatal("invented signature status")
	}
}

func TestSBOMMustDescribeItsBinaryDependencies(t *testing.T) {
	dir := t.TempDir()
	info := &debug.BuildInfo{GoVersion: "go1.27.3", Main: debug.Module{Path: "example.org/main"}, Deps: []*debug.Module{{Path: "example.org/dependency", Version: "v1.2.3"}}}
	for _, test := range []struct {
		name, dependency, standard string
		wantError                  bool
	}{
		{"match", "v1.2.3", "go1.27.3", false},
		{"wrong dependency", "v1.2.4", "go1.27.3", true},
		{"wrong standard library", "v1.2.3", "go1.27.2", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			bom := map[string]any{"bomFormat": "CycloneDX", "specVersion": "1.6",
				"metadata":   map[string]any{"component": map[string]string{"name": info.Main.Path, "version": "1.0.0"}},
				"components": []map[string]string{{"name": "example.org/dependency", "version": test.dependency}, {"name": "std", "version": test.standard}}}
			data, err := json.Marshal(bom)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "bom.json"), data, 0o600); err != nil {
				t.Fatal(err)
			}
			err = checkSBOM(dir, "bom.json", "1.0.0", info)
			if (err != nil) != test.wantError {
				t.Fatalf("unexpected module comparison result: %v", err)
			}
		})
	}
}
