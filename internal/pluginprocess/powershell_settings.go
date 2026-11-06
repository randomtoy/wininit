package pluginprocess

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
)

type PowerShellSettings struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	AllowPrivileged bool     `yaml:"allow_privileged" json:"allow_privileged"`
	ExecutionID     string   `yaml:"execution_id" json:"execution_id"`
	ScriptSHA256    string   `yaml:"script_sha256" json:"script_sha256"`
	MaxOutputBytes  int      `yaml:"max_output_bytes" json:"max_output_bytes"`
	Process         Settings `yaml:"process" json:"process"`
}
type SourceSettings struct {
	Directory       string
	DiscoverVolumes bool
}

func (s PowerShellSettings) Validate() error {
	validHash := func(value string) bool {
		data, err := hex.DecodeString(value)
		return err == nil && len(data) == sha256.Size
	}
	if !s.AllowPrivileged {
		return errors.New("powershell_user_data requires allow_privileged: true to authorize execution under the agent account")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(s.ExecutionID) {
		return errors.New("powershell_user_data.execution_id must contain 1..64 ASCII letters, digits, hyphens, or underscores")
	}
	if !validHash(s.ScriptSHA256) {
		return errors.New("powershell_user_data requires the approved script SHA-256")
	}
	if !identity.LocalAbsolutePath(s.Process.Executable) || !validHash(s.Process.SHA256) || s.Process.TimeoutSeconds < 1 || s.Process.TimeoutSeconds > 3600 {
		return errors.New("powershell_user_data requires an absolute local EXE, exact SHA-256, and timeout_seconds 1..3600")
	}
	if s.MaxOutputBytes < 1024 || s.MaxOutputBytes > 1<<20 {
		return errors.New("powershell_user_data.max_output_bytes must be 1024..1048576")
	}
	return nil
}

type PowerShell struct {
	settings PowerShellSettings
	sources  map[string]SourceSettings
}

func NewPowerShell(settings PowerShellSettings, sources map[string]SourceSettings) *PowerShell {
	copySources := make(map[string]SourceSettings, len(sources))
	for name, source := range sources {
		copySources[name] = source
	}
	return &PowerShell{settings: settings, sources: copySources}
}
func (*PowerShell) Name() string { return "powershell_user_data" }
func (p *PowerShell) ExecutionIdentity(metadata plugin.Metadata) (string, string, error) {
	if err := p.settings.Validate(); err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(p.Name() + "\x00" + metadata.InstanceID + "\x00" + p.settings.ExecutionID))
	return hex.EncodeToString(sum[:]), strings.ToLower(p.settings.ScriptSHA256), nil
}
