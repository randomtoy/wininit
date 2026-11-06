// Package plugin defines the lifecycle's internal plugin contracts.
// Process plugins use the separate versioned sdk/datasource/v1 wire API.
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
	// Datasource is assigned by the host after unambiguous selection.
	Datasource string
}

type Result struct {
	Changed        bool
	RebootRequired bool
	Skipped        bool
	Reused         bool
}

// OnceProvisioner separates read-only preparation from journaled execution.
// Apply must refuse direct execution outside this lifecycle contract.
type OnceProvisioner interface {
	Provisioner
	ExecutionIdentity(Metadata) (key, approvalHash string, err error)
	Prepare(context.Context, Metadata) (PreparedExecution, error)
}

type PreparedExecution struct {
	Skipped bool
	Execute func(context.Context) (ExecutionResult, error)
	Close   func()
}

// ExecutionResult never carries script text, output, or remote error payloads.
type ExecutionResult struct {
	Result
	HasExitCode bool
	ExitCode    uint32
	OutputBytes uint64
	FailureCode string
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
	Reused  bool   `json:"reused,omitempty"`
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
