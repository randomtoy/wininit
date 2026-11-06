// Package datasourcev1 is the experimental v1 SDK for ConfigDrive processes.
// It is independent of wininit's internal lifecycle and state store.
package datasourcev1

import (
	"context"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ProtocolVersion = 1
const PluginKey = "configdrive"
const MaxMessageBytes = 128 << 10

func Handshake() goplugin.HandshakeConfig {
	return goplugin.HandshakeConfig{ProtocolVersion: ProtocolVersion, MagicCookieKey: "WININIT_PLUGIN_PROTOCOL", MagicCookieValue: "wininit-datasource-v1"}
}

type Provider interface {
	Discover(context.Context, *DiscoverRequest) (*DiscoverResponse, error)
}

// GRPCPlugin has no net/rpc fallback. Both the framework handshake and Describe
// identity must match the host's expected protocol, name, and kind.
type GRPCPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Provider Provider
}

func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, server *grpc.Server) error {
	RegisterDatasourceServer(server, &providerServer{provider: p.Provider})
	return nil
}
func (*GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, connection *grpc.ClientConn) (interface{}, error) {
	return NewDatasourceClient(connection), nil
}

type providerServer struct {
	UnimplementedDatasourceServer
	provider Provider
}

func (*providerServer) Describe(context.Context, *DescribeRequest) (*Identity, error) {
	return &Identity{ProtocolVersion: ProtocolVersion, Name: PluginKey, Kind: "datasource"}, nil
}
func (s *providerServer) Discover(ctx context.Context, request *DiscoverRequest) (*DiscoverResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if s.provider == nil {
		return nil, status.Error(codes.FailedPrecondition, "datasource is not configured")
	}
	result, err := s.provider.Discover(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Error(codes.Internal, "datasource discovery failed")
	}
	return result, nil
}

func Serve(provider Provider) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake(),
		Plugins:         goplugin.PluginSet{PluginKey: &GRPCPlugin{Provider: provider}},
		GRPCServer: func(options []grpc.ServerOption) *grpc.Server {
			// go-plugin uses persistent broker/control streams on this connection.
			options = append(options, grpc.MaxRecvMsgSize(MaxMessageBytes), grpc.MaxSendMsgSize(MaxMessageBytes), grpc.MaxConcurrentStreams(16))
			return grpc.NewServer(options...)
		},
		Logger: hclog.NewNullLogger(),
	})
}
