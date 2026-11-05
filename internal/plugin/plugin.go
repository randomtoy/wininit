// Package plugin defines internal contracts for built-in initialization plugins.
// These interfaces are not the future external process protocol or public SDK.
package plugin

import (
	"context"
	"errors"
)

var ErrUnavailable = errors.New("datasource unavailable")

// Metadata contains only fields consumed by the currently implemented plugins.
type Metadata struct {
	InstanceID    string
	LocalHostname string
}

type Result struct {
	Changed        bool
	RebootRequired bool
	Skipped        bool
}

type Datasource interface {
	Name() string
	Discover(context.Context) (Metadata, error)
}

type Provisioner interface {
	Name() string
	Apply(context.Context, Metadata) (Result, error)
}

type DatasourceRegistration struct {
	Enabled bool
	Plugin  Datasource
}

type ProvisionerRegistration struct {
	Enabled bool
	Plugin  Provisioner
}

type Descriptor struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"`
}

type Catalog struct {
	Datasources  []DatasourceRegistration
	Provisioners []ProvisionerRegistration
}

func (c Catalog) Descriptors() []Descriptor {
	result := make([]Descriptor, 0, len(c.Datasources)+len(c.Provisioners))
	for _, registration := range c.Datasources {
		result = append(result, describe(registration.Plugin.Name(), "datasource", registration.Enabled))
	}
	for _, registration := range c.Provisioners {
		result = append(result, describe(registration.Plugin.Name(), "provisioner", registration.Enabled))
	}
	return result
}

func describe(name, kind string, enabled bool) Descriptor {
	status := "disabled"
	if enabled {
		status = "pending"
	}
	return Descriptor{Name: name, Kind: kind, Enabled: enabled, Status: status}
}
