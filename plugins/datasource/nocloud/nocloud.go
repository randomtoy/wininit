// Package nocloud implements the local NoCloud datasource plugin.
package nocloud

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/randomtoy/wininit/internal/plugin"
	"gopkg.in/yaml.v3"
)

const MaxMetadataBytes = 64 << 10

// VolumeFinder returns roots of supported CIDATA volumes. It must not return
// arbitrary network paths or unlabeled drives.
type VolumeFinder func(context.Context) ([]string, error)

type Plugin struct {
	seedDirectory   string
	discoverVolumes bool
	findVolumes     VolumeFinder
}

func New(seedDirectory string, discoverVolumes bool, finder VolumeFinder) *Plugin {
	return &Plugin{seedDirectory: seedDirectory, discoverVolumes: discoverVolumes, findVolumes: finder}
}

func (*Plugin) Name() string { return "nocloud" }

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
			return plugin.Metadata{}, fmt.Errorf("discover NoCloud volumes: %w", err)
		}
		switch len(roots) {
		case 0:
			return plugin.Metadata{}, plugin.ErrUnavailable
		case 1:
			directory = roots[0]
		default:
			return plugin.Metadata{}, errors.New("multiple CIDATA volumes found; select an explicit seed_directory")
		}
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return plugin.Metadata{}, errors.New("open NoCloud seed directory failed")
	}
	defer root.Close()
	info, err := root.Lstat("meta-data")
	if os.IsNotExist(err) {
		return plugin.Metadata{}, plugin.ErrUnavailable
	}
	if err != nil {
		return plugin.Metadata{}, errors.New("inspect NoCloud meta-data failed")
	}
	if !info.Mode().IsRegular() || info.Size() > MaxMetadataBytes {
		return plugin.Metadata{}, errors.New("NoCloud meta-data must be a regular file no larger than 64 KiB")
	}
	file, err := root.Open("meta-data")
	if err != nil {
		return plugin.Metadata{}, errors.New("open NoCloud meta-data failed")
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !openedInfo.Mode().IsRegular() || !os.SameFile(info, openedInfo) {
		return plugin.Metadata{}, errors.New("NoCloud meta-data changed while opening or is not a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxMetadataBytes+1))
	if err != nil {
		return plugin.Metadata{}, errors.New("read NoCloud meta-data failed")
	}
	if err := ctx.Err(); err != nil {
		return plugin.Metadata{}, err
	}
	return Parse(data)
}

// Parse reads the supported subset of NoCloud metadata. Unknown fields are
// ignored for forward compatibility; aliases and multiple documents are denied.
// Parser errors never include input contents that could contain secrets.
func Parse(data []byte) (plugin.Metadata, error) {
	if len(data) > MaxMetadataBytes {
		return plugin.Metadata{}, errors.New("NoCloud meta-data exceeds 64 KiB")
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return plugin.Metadata{}, errors.New("invalid NoCloud metadata YAML")
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		return plugin.Metadata{}, errors.New("NoCloud metadata must contain exactly one document")
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return plugin.Metadata{}, errors.New("NoCloud metadata must be a mapping")
	}
	if err := validateNode(&document, 0); err != nil {
		return plugin.Metadata{}, err
	}
	var result plugin.Metadata
	mapping := document.Content[0]
	seen := make(map[string]bool)
	for i := 0; i < len(mapping.Content); i += 2 {
		key, value := mapping.Content[i], mapping.Content[i+1]
		if key.Tag != "!!str" || seen[key.Value] {
			return plugin.Metadata{}, errors.New("NoCloud metadata has invalid or duplicate keys")
		}
		seen[key.Value] = true
		switch key.Value {
		case "instance-id", "local-hostname":
			if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
				return plugin.Metadata{}, errors.New("NoCloud instance-id and local-hostname must be strings")
			}
			if len(value.Value) > 256 || strings.IndexFunc(value.Value, unicode.IsControl) >= 0 {
				return plugin.Metadata{}, errors.New("NoCloud identifier or hostname is too long or contains control characters")
			}
			if key.Value == "instance-id" {
				result.InstanceID = value.Value
			} else {
				result.LocalHostname = value.Value
			}
		}
	}
	if strings.TrimSpace(result.InstanceID) == "" {
		return plugin.Metadata{}, errors.New("NoCloud instance-id is required")
	}
	return result, nil
}

func validateNode(node *yaml.Node, depth int) error {
	if depth > 32 || node.Kind == yaml.AliasNode {
		return errors.New("NoCloud metadata aliases or excessive nesting are not supported")
	}
	for _, child := range node.Content {
		if err := validateNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
