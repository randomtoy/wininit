package integration

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/randomtoy/wininit/internal/config"
)

func TestInstallerDefaultPluginsAreExplicitlyDisabled(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return the test filename")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "packaging", "wix", "wininit.yaml")
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("config.Load(%q) error = %v", path, err)
	}

	noCloud := cfg.Plugins.Datasources.NoCloud
	if noCloud.Enabled {
		t.Fatal("installer default enabled the NoCloud datasource")
	}
	if noCloud.SeedDirectory != "" {
		t.Fatalf("NoCloud seed_directory = %q, want empty", noCloud.SeedDirectory)
	}
	if !noCloud.DiscoverVolumes {
		t.Fatal("NoCloud discover_volumes = false, want true for explicit future opt-in")
	}
	if cfg.Plugins.Provisioners.Hostname.Enabled {
		t.Fatal("installer default enabled the hostname provisioner")
	}
	configDrive := cfg.Plugins.Datasources.ConfigDrive
	if configDrive.Enabled || configDrive.SeedDirectory != "" || !configDrive.DiscoverVolumes {
		t.Fatalf("unsafe ConfigDrive installer defaults: %#v", configDrive)
	}
	if cfg.Plugins.Directory == "" {
		t.Fatal("installer plugin directory is empty")
	}
}
