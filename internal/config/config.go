// Package config loads and validates wininit configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 1

type Config struct {
	Version int          `yaml:"version" json:"version"`
	Log     LogConfig    `yaml:"log" json:"log"`
	State   StateConfig  `yaml:"state" json:"state"`
	Plugins PluginConfig `yaml:"plugins" json:"plugins"`
	Reboot  RebootConfig `yaml:"reboot" json:"reboot"`
}

type LogConfig struct {
	Level     string `yaml:"level" json:"level"`
	Directory string `yaml:"directory" json:"directory"`
}

type StateConfig struct {
	RegistryPath string `yaml:"registry_path" json:"registry_path"`
}

type PluginConfig struct {
	Directory    string            `yaml:"directory" json:"directory"`
	Datasources  DatasourceConfig  `yaml:"datasources" json:"datasources"`
	Provisioners ProvisionerConfig `yaml:"provisioners" json:"provisioners"`
}

type DatasourceConfig struct {
	NoCloud NoCloudConfig `yaml:"nocloud" json:"nocloud"`
}

type NoCloudConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	SeedDirectory   string `yaml:"seed_directory" json:"seed_directory"`
	DiscoverVolumes bool   `yaml:"discover_volumes" json:"discover_volumes"`
}

type ProvisionerConfig struct {
	Hostname ToggleConfig `yaml:"hostname" json:"hostname"`
}

type ToggleConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type RebootConfig struct {
	Policy string `yaml:"policy" json:"policy"`
}

// Default returns safe settings. Reboots remain disabled because provisioning
// and reboot orchestration are not implemented.
func Default() Config {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		if runtime.GOOS == "windows" {
			programData = `C:\ProgramData`
		} else {
			programData = filepath.Join(os.TempDir(), "wininit")
		}
	}
	return Config{
		Version: CurrentVersion,
		Log:     LogConfig{Level: "info", Directory: filepath.Join(programData, "wininit", "logs")},
		State:   StateConfig{RegistryPath: `SOFTWARE\Wininit`},
		Plugins: PluginConfig{Directory: filepath.Join(programData, "wininit", "plugins"), Datasources: DatasourceConfig{NoCloud: NoCloudConfig{DiscoverVolumes: true}}},
		Reboot:  RebootConfig{Policy: "never"},
	}
}

func DefaultPath() string {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		if runtime.GOOS == "windows" {
			programData = `C:\ProgramData`
		} else {
			programData = filepath.Join(os.TempDir(), "wininit")
		}
	}
	return filepath.Join(programData, "wininit", "wininit.yaml")
}

// Load strictly decodes and validates a YAML configuration.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read configuration %q: %w", path, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return Config{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if !hasExplicitIntegerVersion(&document) {
		return Config{}, fmt.Errorf("validate configuration %q: version must be explicitly specified as an integer", path)
	}
	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple YAML documents are not allowed")
		}
		return Config{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate configuration %q: %w", path, err)
	}
	return cfg, nil
}

func hasExplicitIntegerVersion(document *yaml.Node) bool {
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return false
	}
	mapping := document.Content[0]
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == "version" {
			return mapping.Content[index+1].Tag == "!!int"
		}
	}
	return false
}

func (c Config) Validate() error {
	var errs []error
	if c.Version != CurrentVersion {
		errs = append(errs, fmt.Errorf("version must be %d, got %d", CurrentVersion, c.Version))
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, errors.New("log.level must be one of debug, info, warn, error"))
	}
	if strings.TrimSpace(c.Log.Directory) == "" {
		errs = append(errs, errors.New("log.directory must not be empty"))
	}
	path := strings.TrimSpace(c.State.RegistryPath)
	if path == "" {
		errs = append(errs, errors.New("state.registry_path must not be empty"))
	} else if !strings.HasPrefix(strings.ToUpper(path), `SOFTWARE\`) {
		errs = append(errs, errors.New(`state.registry_path must be below SOFTWARE\`))
	}
	if strings.TrimSpace(c.Plugins.Directory) == "" {
		errs = append(errs, errors.New("plugins.directory must not be empty"))
	}
	noCloud := c.Plugins.Datasources.NoCloud
	if noCloud.Enabled {
		if noCloud.SeedDirectory == "" && !noCloud.DiscoverVolumes {
			errs = append(errs, errors.New("enabled nocloud requires seed_directory or discover_volumes"))
		}
		if noCloud.SeedDirectory != "" && !filepath.IsAbs(noCloud.SeedDirectory) {
			errs = append(errs, errors.New("plugins.datasources.nocloud.seed_directory must be an absolute local path"))
		}
		if strings.HasPrefix(noCloud.SeedDirectory, `\\`) || strings.HasPrefix(noCloud.SeedDirectory, "//") || strings.Contains(noCloud.SeedDirectory, "://") {
			errs = append(errs, errors.New("nocloud network paths and URLs are not supported"))
		}
	}
	if c.Reboot.Policy != "never" {
		errs = append(errs, errors.New(`reboot.policy must be "never" until reboot orchestration is implemented`))
	}
	return errors.Join(errs...)
}

// Sanitized is an explicit allowlist for diagnostic output.
func (c Config) Sanitized() Config {
	return Config{Version: c.Version, Log: c.Log, State: c.State, Plugins: c.Plugins, Reboot: c.Reboot}
}
