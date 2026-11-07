// Package hostnameprocess adapts idempotent hostname reconciliation to process RPC.
package hostnameprocess

import (
	"context"
	"errors"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/plugins/provisioner/hostname"
	provisionerv1 "github.com/randomtoy/wininit/sdk/provisioner/v1"
)

type Provider struct{ Platform hostname.Platform }

func (p Provider) Apply(ctx context.Context, request *provisionerv1.ApplyRequest) (*provisionerv1.ApplyResponse, error) {
	if err := provisionerv1.ValidateMetadata(request.GetMetadata()); err != nil {
		return nil, err
	}
	if p.Platform == nil {
		return nil, errors.New("hostname platform is not configured")
	}
	input := request.GetMetadata()
	value, err := hostname.New(p.Platform).Apply(ctx, plugin.Metadata{InstanceID: input.InstanceId, LocalHostname: input.LocalHostname, Datasource: input.Datasource})
	if err != nil {
		return nil, err
	}
	outcome := provisionerv1.Outcome_UNCHANGED
	switch {
	case value.Skipped:
		outcome = provisionerv1.Outcome_SKIPPED
	case value.Changed && value.RebootRequired:
		outcome = provisionerv1.Outcome_CHANGED_REBOOT_REQUIRED
	case value.RebootRequired:
		outcome = provisionerv1.Outcome_REBOOT_REQUIRED
	case value.Changed:
		outcome = provisionerv1.Outcome_CHANGED
	}
	return &provisionerv1.ApplyResponse{Outcome: outcome}, nil
}
