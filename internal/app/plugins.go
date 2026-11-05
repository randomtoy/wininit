package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/randomtoy/wininit/internal/config"
	platformhostname "github.com/randomtoy/wininit/internal/platform/hostname"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/plugins/datasource/nocloud"
	"github.com/randomtoy/wininit/plugins/provisioner/hostname"
)

// configuredPlugins only constructs plugin objects; constructors perform no IO.
// The lifecycle consults Enabled before calling Discover or Apply.
func configuredPlugins(cfg config.Config) plugin.Catalog {
	return plugin.Catalog{
		Datasources: []plugin.DatasourceRegistration{{
			Enabled: cfg.Plugins.Datasources.NoCloud.Enabled,
			Plugin:  nocloud.New(cfg.Plugins.Datasources.NoCloud.SeedDirectory, cfg.Plugins.Datasources.NoCloud.DiscoverVolumes, nocloud.FindVolumes),
		}},
		Provisioners: []plugin.ProvisionerRegistration{{Enabled: cfg.Plugins.Provisioners.Hostname.Enabled, Plugin: hostname.New(platformhostname.New())}},
	}
}

func pluginConfigurationHash(cfg config.Config) string {
	data, _ := json.Marshal(cfg.Plugins)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func configuredProvisioning(cfg config.Config) string {
	if !cfg.Plugins.Datasources.NoCloud.Enabled && !cfg.Plugins.Provisioners.Hostname.Enabled {
		return "disabled"
	}
	return "pending"
}
