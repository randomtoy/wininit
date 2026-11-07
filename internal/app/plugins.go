package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/randomtoy/wininit/internal/config"
	platformhostname "github.com/randomtoy/wininit/internal/platform/hostname"
	platformidentity "github.com/randomtoy/wininit/internal/platform/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/pluginprocess"
	"github.com/randomtoy/wininit/plugins/datasource/configdrive"
	"github.com/randomtoy/wininit/plugins/datasource/nocloud"
	"github.com/randomtoy/wininit/plugins/provisioner/authorizedkeys"
	"github.com/randomtoy/wininit/plugins/provisioner/hostname"
	"github.com/randomtoy/wininit/plugins/provisioner/localusers"
	"github.com/randomtoy/wininit/plugins/provisioner/passwordpolicy"
)

// configuredPlugins only constructs plugin objects; constructors perform no IO.
// The lifecycle consults Enabled before calling Discover or Apply.
func configuredPlugins(cfg config.Config) plugin.Catalog {
	drive := cfg.Plugins.Datasources.ConfigDrive
	var drivePlugin plugin.Datasource = configdrive.New(drive.SeedDirectory, drive.DiscoverVolumes, configdrive.FindVolumes)
	if drive.Execution == "external" {
		drivePlugin = pluginprocess.NewConfigDrive(drive.Process, drive.SeedDirectory, drive.DiscoverVolumes)
	}
	cloud := cfg.Plugins.Datasources.NoCloud
	var cloudPlugin plugin.Datasource = nocloud.New(cloud.SeedDirectory, cloud.DiscoverVolumes, nocloud.FindVolumes)
	if cloud.Execution == "external" {
		cloudPlugin = pluginprocess.NewNoCloud(cloud.Process, cloud.SeedDirectory, cloud.DiscoverVolumes)
	}
	var hostnamePlugin plugin.Provisioner = hostname.New(platformhostname.New())
	if cfg.Plugins.Provisioners.Hostname.Execution == "external" {
		hostnamePlugin = pluginprocess.NewHostname(cfg.Plugins.Provisioners.Hostname.ProvisionerSettings)
	}
	accounts := platformidentity.New()
	return plugin.Catalog{
		Datasources: []plugin.DatasourceRegistration{{
			Enabled: cfg.Plugins.Datasources.NoCloud.Enabled,
			Plugin:  cloudPlugin,
		}, {
			Enabled: cfg.Plugins.Datasources.ConfigDrive.Enabled,
			Plugin:  drivePlugin,
		}},
		Provisioners: []plugin.ProvisionerRegistration{
			{Enabled: cfg.Plugins.Provisioners.PasswordPolicy.Enabled, Plugin: passwordpolicy.New(cfg.Plugins.Provisioners.PasswordPolicy.PasswordPolicy, accounts)},
			{Enabled: cfg.Plugins.Provisioners.LocalUsers.Enabled, Plugin: localusers.New(cfg.Plugins.Provisioners.LocalUsers.Users, accounts)},
			{Enabled: cfg.Plugins.Provisioners.AuthorizedKeys.Enabled, Plugin: authorizedkeys.New(cfg.Plugins.Provisioners.AuthorizedKeys.Users, accounts)},
			{Enabled: cfg.Plugins.Provisioners.Hostname.Enabled, Plugin: hostnamePlugin},
			{Enabled: cfg.Plugins.Provisioners.PowerShellUserData.Enabled, Plugin: pluginprocess.NewPowerShell(cfg.Plugins.Provisioners.PowerShellUserData, map[string]pluginprocess.SourceSettings{
				"nocloud":     {Directory: cfg.Plugins.Datasources.NoCloud.SeedDirectory, DiscoverVolumes: cfg.Plugins.Datasources.NoCloud.DiscoverVolumes},
				"configdrive": {Directory: drive.SeedDirectory, DiscoverVolumes: drive.DiscoverVolumes},
			})},
		},
	}
}

func pluginConfigurationHash(cfg config.Config) string {
	data, _ := json.Marshal(cfg.Plugins)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func configuredProvisioning(cfg config.Config) string {
	if !cfg.Plugins.Datasources.NoCloud.Enabled && !cfg.Plugins.Datasources.ConfigDrive.Enabled && !cfg.Plugins.Provisioners.Hostname.Enabled &&
		!cfg.Plugins.Provisioners.LocalUsers.Enabled && !cfg.Plugins.Provisioners.PasswordPolicy.Enabled && !cfg.Plugins.Provisioners.AuthorizedKeys.Enabled && !cfg.Plugins.Provisioners.PowerShellUserData.Enabled {
		return "disabled"
	}
	return "pending"
}
