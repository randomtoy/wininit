package powershell

import (
	"context"
	"errors"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/plugins/datasource/configdrive"
	"github.com/randomtoy/wininit/plugins/datasource/nocloud"
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
)

var errMissing = errors.New("user-data file is absent")

type directory interface {
	Subdir(string) (directory, error)
	Read(string, int) ([]byte, error)
	Close() error
}

func readUserData(ctx context.Context, request *userdatav1.PrepareRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	kind := request.GetDatasource()
	if kind != "nocloud" && kind != "configdrive" {
		return nil, userdatav1.ErrSource
	}
	path := request.GetSeedDirectory()
	if path == "" {
		if !request.GetDiscoverVolumes() {
			return nil, userdatav1.ErrSource
		}
		var roots []string
		var err error
		if kind == "nocloud" {
			roots, err = nocloud.FindVolumes(ctx)
		} else {
			roots, err = configdrive.FindVolumes(ctx)
		}
		if err != nil || len(roots) != 1 {
			return nil, userdatav1.ErrSource
		}
		path = roots[0]
	}
	if !identity.LocalAbsolutePath(path) {
		return nil, userdatav1.ErrSource
	}
	root, err := openDirectory(path)
	if err != nil {
		return nil, userdatav1.ErrSource
	}
	defer root.Close()
	selected := root
	metadataName, userDataName := "meta-data", "user-data"
	var parse func([]byte) (plugin.Metadata, error) = nocloud.Parse
	if kind == "configdrive" {
		openstack, err := root.Subdir("openstack")
		if err != nil {
			return nil, userdatav1.ErrSource
		}
		defer openstack.Close()
		var versionDir directory
		for _, version := range configdrive.SupportedVersions() {
			candidate, err := openstack.Subdir(version)
			if errors.Is(err, errMissing) {
				continue
			}
			if err != nil {
				return nil, userdatav1.ErrSource
			}
			versionDir = candidate
			break
		}
		if versionDir == nil {
			return nil, userdatav1.ErrSource
		}
		defer versionDir.Close()
		selected = versionDir
		metadataName, userDataName = "meta_data.json", "user_data"
		parse = configdrive.Parse
	}
	data, err := selected.Read(metadataName, 64<<10)
	if err != nil {
		return nil, userdatav1.ErrSource
	}
	metadata, parseErr := parse(data)
	clear(data)
	if parseErr != nil || metadata.InstanceID != request.GetExpectedInstanceId() || metadata.LocalHostname != request.GetExpectedHostname() {
		return nil, userdatav1.ErrSource
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	script, err := selected.Read(userDataName, userdatav1.MaxScriptBytes)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		clear(script)
		return nil, err
	}
	return script, nil
}
