package main

import (
	"context"
	"errors"

	"github.com/randomtoy/wininit/internal/identity"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/plugins/datasource/configdrive"
	datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
)

type provider struct{}

func (provider) Discover(ctx context.Context, request *datasourcev1.DiscoverRequest) (*datasourcev1.DiscoverResponse, error) {
	if request.GetSeedDirectory() != "" && !identity.LocalAbsolutePath(request.GetSeedDirectory()) {
		return nil, errors.New("invalid seed directory")
	}
	metadata, err := configdrive.New(request.GetSeedDirectory(), request.GetDiscoverVolumes(), configdrive.FindVolumes).Discover(ctx)
	if errors.Is(err, plugin.ErrUnavailable) {
		return &datasourcev1.DiscoverResponse{Unavailable: true}, nil
	}
	if err != nil {
		return nil, err
	}
	return &datasourcev1.DiscoverResponse{Metadata: &datasourcev1.Metadata{InstanceId: metadata.InstanceID, LocalHostname: metadata.LocalHostname}}, nil
}
func main() { datasourcev1.Serve(provider{}) }
