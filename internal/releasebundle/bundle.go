// Package releasebundle inventories the fixed Windows distribution. It does
// not establish publisher trust: authenticate SHA256SUMS with an attestation.
package releasebundle

import (
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
)

const ManifestName = "release-manifest.json"

var executables = []string{
	"wininit-windows-amd64.exe",
	"wininit-plugin-configdrive-windows-amd64.exe",
	"wininit-plugin-powershell-windows-amd64.exe",
	"wininit-plugin-nocloud-windows-amd64.exe",
	"wininit-plugin-hostname-windows-amd64.exe",
}

// Names returns the exact checksum subjects in canonical order. Neither the
// checksum file itself nor later Sigstore bundles are recursive subjects.
func Names() []string {
	names := append([]string{}, executables...)
	names = append(names, "wininit-windows-amd64.msi")
	for _, name := range executables {
		names = append(names, name+".cdx.json")
	}
	return append(names, ManifestName)
}

type Artifact struct {
	Name      string `json:"name"`
	SHA256    string `json:"sha256"`
	GoVersion string `json:"go_version,omitempty"`
	Main      string `json:"main,omitempty"`
	SBOM      string `json:"sbom,omitempty"`
}

type Manifest struct {
	SchemaVersion int        `json:"schema_version"`
	Version       string     `json:"version"`
	Commit        string     `json:"commit"`
	Modified      bool       `json:"source_modified"`
	Target        string     `json:"target"`
	Signature     string     `json:"authenticode"`
	SBOMScope     string     `json:"sbom_scope"`
	Artifacts     []Artifact `json:"artifacts"`
}

var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)
var versionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func validateOptions(version, commit, signature string) error {
	if !versionPattern.MatchString(version) || !commitPattern.MatchString(commit) {
		return errors.New("require numeric MAJOR.MINOR.PATCH version and full lowercase source commit")
	}
	if signature != "unsigned" && signature != "authenticode" {
		return errors.New("authenticode status must be unsigned or authenticode")
	}
	return nil
}

func hashFile(dir, name string) (string, error) {
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", name)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func inspect(info *debug.BuildInfo, main, commit string) (bool, error) {
	settings := make(map[string]string)
	for _, item := range info.Settings {
		settings[item.Key] = item.Value
	}
	if info.Path != main || info.Main.Path != "github.com/randomtoy/wininit" ||
		settings["GOOS"] != "windows" || settings["GOARCH"] != "amd64" || settings["CGO_ENABLED"] != "0" ||
		settings["vcs.revision"] != commit || settings["vcs"] != "git" ||
		(settings["vcs.modified"] != "true" && settings["vcs.modified"] != "false") {
		return false, errors.New("unexpected main module, Windows target, CGO, or source revision")
	}
	return settings["vcs.modified"] == "true", nil
}

func checkSBOM(dir, name, version string, info *debug.BuildInfo) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	var bom struct {
		Format   string `json:"bomFormat"`
		Spec     string `json:"specVersion"`
		Metadata struct {
			Component struct {
				Version string `json:"version"`
				Name    string `json:"name"`
			} `json:"component"`
		} `json:"metadata"`
		Components []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &bom); err != nil {
		return fmt.Errorf("decode SBOM: %w", err)
	}
	if bom.Format != "CycloneDX" || bom.Spec != "1.6" || bom.Metadata.Component.Version != version ||
		bom.Metadata.Component.Name != info.Main.Path || len(bom.Components) == 0 {
		return errors.New("require CycloneDX 1.6 module inventory with matching release version")
	}
	expected := map[string]string{"std": info.GoVersion}
	for _, dependency := range info.Deps {
		if dependency.Replace != nil {
			return errors.New("replaced modules require a reviewed SBOM policy")
		}
		expected[dependency.Path] = dependency.Version
	}
	for _, component := range bom.Components {
		version, exists := expected[component.Name]
		if !exists || component.Version != version {
			return errors.New("SBOM module set differs from binary build info")
		}
		delete(expected, component.Name)
	}
	if len(expected) != 0 {
		return errors.New("SBOM omits binary dependencies")
	}
	return nil
}

// Inventory binds exact binaries and their externally generated SBOMs. MSI
// construction and Authenticode verification belong to the Windows packager.
func Inventory(dir, version, commit, signature string, requireClean bool) (Manifest, error) {
	m := Manifest{SchemaVersion: 1, Version: version, Commit: commit, Target: "windows/amd64", Signature: signature,
		SBOMScope: "Per-EXE Go build-info module inventories including stdlib; MSI contains the agent EXE and default configuration. No OS, WiX, license, or vulnerability completeness claim."}
	if err := validateOptions(version, commit, signature); err != nil {
		return m, err
	}
	for _, name := range Names() {
		if name == ManifestName {
			continue
		}
		hash, err := hashFile(dir, name)
		if err != nil {
			return m, fmt.Errorf("hash %s: %w", name, err)
		}
		artifact := Artifact{Name: name, SHA256: hash}
		if slices.Contains(executables, name) {
			main := strings.TrimSuffix(strings.TrimSuffix(name, "-windows-amd64.exe"), ".exe")
			artifact.Main = "github.com/randomtoy/wininit/cmd/" + main
			info, err := buildinfo.ReadFile(filepath.Join(dir, name))
			if err != nil {
				return m, fmt.Errorf("read %s build info: %w", name, err)
			}
			modified, err := inspect(info, artifact.Main, commit)
			if err != nil {
				return m, fmt.Errorf("validate %s: %w", name, err)
			}
			m.Modified = m.Modified || modified
			artifact.GoVersion = info.GoVersion
			artifact.SBOM = name + ".cdx.json"
			if err := checkSBOM(dir, artifact.SBOM, version, info); err != nil {
				return m, fmt.Errorf("validate %s: %w", artifact.SBOM, err)
			}
		}
		m.Artifacts = append(m.Artifacts, artifact)
	}
	if requireClean && m.Modified {
		return m, errors.New("release requires a clean source checkout")
	}
	return m, nil
}

func checksumContent(dir string) ([]byte, error) {
	var result strings.Builder
	for _, name := range Names() {
		hash, err := hashFile(dir, name)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&result, "%s  %s\n", hash, name)
	}
	return []byte(result.String()), nil
}

func Generate(dir, version, commit, signature string, requireClean bool) error {
	m, err := Inventory(dir, version, commit, signature, requireClean)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), append(data, '\n'), 0o644); err != nil {
		return err
	}
	checksums, err := checksumContent(dir)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "SHA256SUMS"), checksums, 0o644)
}

// Verify checks content integrity and rebuilds the inventory from the binary
// build information. It does not verify Authenticode or GitHub attestations.
func Verify(dir string, requireClean bool) error {
	data, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		return err
	}
	var actual Manifest
	if err := json.Unmarshal(data, &actual); err != nil {
		return err
	}
	expected, err := Inventory(dir, actual.Version, actual.Commit, actual.Signature, requireClean)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, actual) {
		return errors.New("release inventory differs from artifact contents")
	}
	expectedChecksums, err := checksumContent(dir)
	if err != nil {
		return err
	}
	checksums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	if string(checksums) != string(expectedChecksums) {
		return errors.New("SHA256SUMS differs from the exact canonical release subjects")
	}
	return nil
}
