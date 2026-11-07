// Package nocloudprocess adapts the reviewed NoCloud reader to datasource v1.
package nocloudprocess

import (
	"context"
	"errors"
	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/plugins/datasource/nocloud"
	datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
)

type Provider struct{}

func (Provider) Discover(ctx context.Context, request *datasourcev1.DiscoverRequest) (*datasourcev1.DiscoverResponse, error) {
	if request.GetSeedDirectory() != "" && !identity.LocalAbsolutePath(request.GetSeedDirectory()) {
		return nil, errors.New("invalid seed directory")
	}
	value, err := nocloud.New(request.GetSeedDirectory(), request.GetDiscoverVolumes(), nocloud.FindVolumes).Discover(ctx)
	if errors.Is(err, plugin.ErrUnavailable) {
		return &datasourcev1.DiscoverResponse{Unavailable: true}, nil
	}
	if err != nil {
		return nil, err
	}
	return &datasourcev1.DiscoverResponse{Metadata: &datasourcev1.Metadata{InstanceId: value.InstanceID, LocalHostname: value.LocalHostname}}, nil
}
