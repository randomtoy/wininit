// Package configdrive implements the local OpenStack ConfigDrive datasource.
package configdrive

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/randomtoy/wininit/internal/platform/seedmedia"
	"github.com/randomtoy/wininit/internal/plugin"
)

const MaxMetadataBytes = 64 << 10

type VolumeFinder func(context.Context) ([]string, error)

type Plugin struct {
	seedDirectory   string
	discoverVolumes bool
	findVolumes     VolumeFinder
}

var _ plugin.Datasource = (*Plugin)(nil)

// New performs no I/O; the lifecycle enablement gate controls execution.
func New(seedDirectory string, discoverVolumes bool, finder VolumeFinder) *Plugin {
	return &Plugin{seedDirectory: seedDirectory, discoverVolumes: discoverVolumes, findVolumes: finder}
}

func (*Plugin) Name() string { return "configdrive" }

func FindVolumes(ctx context.Context) ([]string, error) {
	return seedmedia.FindVolumes(ctx, "config-2")
}

func (p *Plugin) Discover(ctx context.Context) (plugin.Metadata, error) {
	if err := ctx.Err(); err != nil {
		return plugin.Metadata{}, err
	}
	directory := p.seedDirectory
	if directory == "" {
		if !p.discoverVolumes || p.findVolumes == nil {
			return plugin.Metadata{}, plugin.ErrUnavailable
		}
		roots, err := p.findVolumes(ctx)
		if err != nil {
			return plugin.Metadata{}, fmt.Errorf("discover ConfigDrive volumes: %w", err)
		}
		switch len(roots) {
		case 0:
			return plugin.Metadata{}, plugin.ErrUnavailable
		case 1:
			directory = roots[0]
		default:
			return plugin.Metadata{}, errors.New("multiple config-2 volumes found; select an explicit seed_directory")
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return plugin.Metadata{}, errors.New("open ConfigDrive seed directory failed")
	}
	defer root.Close()
	path, err := metadataPath(root)
	if err != nil {
		return plugin.Metadata{}, err
	}
	if err := ctx.Err(); err != nil {
		return plugin.Metadata{}, err
	}
	info, err := root.Lstat(path)
	if err != nil {
		return plugin.Metadata{}, errors.New("selected ConfigDrive version has no readable meta_data.json")
	}
	if !info.Mode().IsRegular() || info.Size() > MaxMetadataBytes {
		return plugin.Metadata{}, errors.New("ConfigDrive meta_data.json must be a regular file no larger than 64 KiB")
	}
	file, err := root.Open(path)
	if err != nil {
		return plugin.Metadata{}, errors.New("open ConfigDrive meta_data.json failed")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return plugin.Metadata{}, errors.New("ConfigDrive meta_data.json changed while opening or is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxMetadataBytes+1))
	if err != nil {
		return plugin.Metadata{}, errors.New("read ConfigDrive meta_data.json failed")
	}
	if err := ctx.Err(); err != nil {
		return plugin.Metadata{}, err
	}
	return Parse(data)
}

// metadataPath selects the newest supported dated directory, falling back to
// latest only when no supported dated directory exists. A malformed selected
// version is an error, never a reason to consume older metadata silently.
func metadataPath(root *os.Root) (string, error) {
	info, err := root.Lstat("openstack")
	if os.IsNotExist(err) {
		return "", plugin.ErrUnavailable
	}
	if err != nil || !info.IsDir() {
		return "", errors.New("ConfigDrive openstack must be a readable directory, not a symlink")
	}
	for _, version := range SupportedVersions() {
		path := filepath.Join("openstack", version)
		info, err := root.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() {
			return "", errors.New("ConfigDrive version must be a readable directory, not a symlink")
		}
		return filepath.Join(path, "meta_data.json"), nil
	}
	return "", plugin.ErrUnavailable
}

// SupportedVersions returns a fresh ordered list shared with the opt-in
// user-data reader. Datasource discovery itself still never opens user_data.
func SupportedVersions() []string {
	return []string{"2025-04-04", "2020-10-14", "2018-08-27", "2017-02-22", "2016-10-06", "2016-06-30", "2015-10-15", "2013-10-17", "2013-04-04", "2012-08-10", "latest"}
}
