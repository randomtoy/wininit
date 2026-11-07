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

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/pluginprocess"
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
	NoCloud     ProcessDatasourceConfig `yaml:"nocloud" json:"nocloud"`
	ConfigDrive ProcessDatasourceConfig `yaml:"configdrive" json:"configdrive"`
}

type ProcessDatasourceConfig struct {
	LocalDatasourceConfig `yaml:",inline"`
	Execution             string                 `yaml:"execution" json:"execution"`
	Process               pluginprocess.Settings `yaml:"process" json:"process"`
}
type ConfigDriveConfig = ProcessDatasourceConfig

type LocalDatasourceConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	SeedDirectory   string `yaml:"seed_directory" json:"seed_directory"`
	DiscoverVolumes bool   `yaml:"discover_volumes" json:"discover_volumes"`
}

type ProvisionerConfig struct {
	Hostname           HostnameConfig                   `yaml:"hostname" json:"hostname"`
	LocalUsers         LocalUsersConfig                 `yaml:"local_users" json:"local_users"`
	PasswordPolicy     PasswordPolicyConfig             `yaml:"password_policy" json:"password_policy"`
	AuthorizedKeys     AuthorizedKeysConfig             `yaml:"authorized_keys" json:"authorized_keys"`
	PowerShellUserData pluginprocess.PowerShellSettings `yaml:"powershell_user_data" json:"powershell_user_data"`
}

type LocalUsersConfig struct {
	Enabled bool            `yaml:"enabled" json:"enabled"`
	Users   []identity.User `yaml:"users" json:"users"`
}
type PasswordPolicyConfig struct {
	Enabled                 bool `yaml:"enabled" json:"enabled"`
	identity.PasswordPolicy `yaml:",inline"`
}
type AuthorizedKeysConfig struct {
	Enabled bool               `yaml:"enabled" json:"enabled"`
	Users   []identity.KeyUser `yaml:"users" json:"users"`
}
type HostnameConfig struct {
	Enabled                           bool   `yaml:"enabled" json:"enabled"`
	Execution                         string `yaml:"execution" json:"execution"`
	pluginprocess.ProvisionerSettings `yaml:",inline"`
}
type ToggleConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type RebootConfig struct {
	Policy string `yaml:"policy" json:"policy"`
}

// Default keeps every plugin opt-in and leaves reboots administrator-controlled.
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
		Plugins: PluginConfig{
			Directory: filepath.Join(programData, "wininit", "plugins"),
			Datasources: DatasourceConfig{
				NoCloud:     ConfigDriveConfig{LocalDatasourceConfig: LocalDatasourceConfig{DiscoverVolumes: true}, Execution: "builtin", Process: pluginprocess.Settings{TimeoutSeconds: 15}},
				ConfigDrive: ConfigDriveConfig{LocalDatasourceConfig: LocalDatasourceConfig{DiscoverVolumes: true}, Execution: "builtin", Process: pluginprocess.Settings{TimeoutSeconds: 15}},
			},
			Provisioners: ProvisionerConfig{Hostname: HostnameConfig{Execution: "builtin", ProvisionerSettings: pluginprocess.ProvisionerSettings{Process: pluginprocess.Settings{TimeoutSeconds: 30}}}, PasswordPolicy: PasswordPolicyConfig{PasswordPolicy: identity.DefaultPasswordPolicy()}, PowerShellUserData: pluginprocess.PowerShellSettings{MaxOutputBytes: 64 << 10, Process: pluginprocess.Settings{TimeoutSeconds: 120}}},
		},
		Reboot: RebootConfig{Policy: "never"},
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
	if len(path) <= len(`SOFTWARE\`) || strings.HasSuffix(path, `\`) || strings.Contains(path, `\\`) || strings.ContainsAny(path, "/\x00") || path != c.State.RegistryPath {
		errs = append(errs, errors.New("state.registry_path must name a nonempty subkey with no empty path components, NUL, or surrounding whitespace"))
	}
	if strings.TrimSpace(c.Plugins.Directory) == "" {
		errs = append(errs, errors.New("plugins.directory must not be empty"))
	}
	for _, entry := range []struct {
		name   string
		source LocalDatasourceConfig
	}{{"nocloud", c.Plugins.Datasources.NoCloud.LocalDatasourceConfig}, {"configdrive", c.Plugins.Datasources.ConfigDrive.LocalDatasourceConfig}} {
		name, source := entry.name, entry.source
		if source.Enabled {
			if source.SeedDirectory == "" && !source.DiscoverVolumes {
				errs = append(errs, fmt.Errorf("enabled %s requires seed_directory or discover_volumes", name))
			}
			if source.SeedDirectory != "" && !filepath.IsAbs(source.SeedDirectory) {
				errs = append(errs, fmt.Errorf("plugins.datasources.%s.seed_directory must be an absolute local path", name))
			}
			if strings.HasPrefix(source.SeedDirectory, `\\`) || strings.HasPrefix(source.SeedDirectory, "//") || strings.Contains(source.SeedDirectory, "://") {
				errs = append(errs, fmt.Errorf("%s network paths and URLs are not supported", name))
			}
		}
	}
	for _, entry := range []struct {
		name   string
		source ConfigDriveConfig
	}{{"nocloud", c.Plugins.Datasources.NoCloud}, {"configdrive", c.Plugins.Datasources.ConfigDrive}} {
		if entry.source.Enabled {
			switch entry.source.Execution {
			case "builtin":
			case "external":
				errs = append(errs, entry.source.Process.Validate())
			default:
				errs = append(errs, fmt.Errorf("%s.execution must be builtin or external", entry.name))
			}
		}
	}
	if c.Plugins.Provisioners.Hostname.Enabled {
		switch c.Plugins.Provisioners.Hostname.Execution {
		case "builtin":
		case "external":
			errs = append(errs, c.Plugins.Provisioners.Hostname.ProvisionerSettings.Validate())
		default:
			errs = append(errs, errors.New("hostname.execution must be builtin or external"))
		}
	}
	if c.Reboot.Policy != "never" {
		errs = append(errs, errors.New(`reboot.policy must be "never" until reboot orchestration is implemented`))
	}
	if c.Plugins.Provisioners.LocalUsers.Enabled {
		errs = append(errs, identity.ValidateUsers(c.Plugins.Provisioners.LocalUsers.Users))
		for _, user := range c.Plugins.Provisioners.LocalUsers.Users {
			relative, err := filepath.Rel(c.Log.Directory, user.PasswordFile)
			if user.PasswordFile != "" && err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				errs = append(errs, errors.New("local_users password_file must be outside log.directory"))
			}
		}
	}
	if c.Plugins.Provisioners.PasswordPolicy.Enabled {
		errs = append(errs, c.Plugins.Provisioners.PasswordPolicy.PasswordPolicy.Validate())
	}
	if c.Plugins.Provisioners.AuthorizedKeys.Enabled {
		errs = append(errs, identity.ValidateKeyUsers(c.Plugins.Provisioners.AuthorizedKeys.Users))
	}
	if c.Plugins.Provisioners.PowerShellUserData.Enabled {
		errs = append(errs, c.Plugins.Provisioners.PowerShellUserData.Validate())
	}
	return errors.Join(errs...)
}

// Sanitized is an explicit allowlist for diagnostic output.
func (c Config) Sanitized() Config {
	safe := c
	safe.Plugins.Provisioners.LocalUsers.Users = nil
	safe.Plugins.Provisioners.AuthorizedKeys.Users = nil
	safe.Plugins.Provisioners.PowerShellUserData.ExecutionID = ""
	return safe
}
