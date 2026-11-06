// Package userdatav1 is the experimental PowerShell-only process SDK.
package userdatav1

import (
	"context"
	"errors"
	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"time"
)

const ProtocolVersion = 1
const PluginKey = "powershell_user_data"
const MaxMessageBytes = 128 << 10
const MaxScriptBytes = 1 << 20
const MaxOutputBytes = 1 << 20

var (
	ErrInvalidData        = errors.New("invalid PowerShell user-data")
	ErrApproval           = errors.New("PowerShell user-data is not approved")
	ErrSource             = errors.New("selected source identity changed")
	ErrUnavailableRuntime = errors.New("PowerShell runtime is unavailable")
)

func Handshake() goplugin.HandshakeConfig {
	return goplugin.HandshakeConfig{ProtocolVersion: ProtocolVersion, MagicCookieKey: "WININIT_PLUGIN_PROTOCOL", MagicCookieValue: "wininit-userdata-v1"}
}

type Provider interface {
	Prepare(context.Context, *PrepareRequest) (*Prepared, error)
	Execute(context.Context, *ExecuteRequest) (*Execution, error)
}
type GRPCPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Provider Provider
}

func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, server *grpc.Server) error {
	RegisterUserDataServer(server, &providerServer{provider: p.Provider})
	return nil
}
func (*GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, connection *grpc.ClientConn) (interface{}, error) {
	return NewUserDataClient(connection), nil
}

type providerServer struct {
	UnimplementedUserDataServer
	provider Provider
}

func (*providerServer) Describe(context.Context, *DescribeRequest) (*Identity, error) {
	return &Identity{ProtocolVersion: ProtocolVersion, Name: PluginKey, Kind: "provisioner"}, nil
}
func (s *providerServer) Prepare(ctx context.Context, request *PrepareRequest) (*Prepared, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if s.provider == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider is not configured")
	}
	result, err := s.provider.Prepare(ctx, request)
	if err != nil {
		return nil, safeError(ctx, err, "preparation failed")
	}
	return result, nil
}
func (s *providerServer) Execute(ctx context.Context, request *ExecuteRequest) (*Execution, error) {
	if request.GetTimeoutSeconds() < 1 || request.GetTimeoutSeconds() > 3600 || request.GetMaxOutputBytes() < 1024 || request.GetMaxOutputBytes() > MaxOutputBytes {
		return nil, status.Error(codes.InvalidArgument, "execution limits are invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(request.GetTimeoutSeconds())*time.Second)
	defer cancel()
	if s.provider == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider is not configured")
	}
	result, err := s.provider.Execute(ctx, request)
	if err != nil {
		return nil, safeError(ctx, err, "execution failed")
	}
	return result, nil
}
func safeError(ctx context.Context, cause error, message string) error {
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	switch {
	case errors.Is(cause, ErrInvalidData):
		return status.Error(codes.InvalidArgument, "invalid user-data")
	case errors.Is(cause, ErrApproval):
		return status.Error(codes.PermissionDenied, "script is not approved")
	case errors.Is(cause, ErrSource), errors.Is(cause, ErrUnavailableRuntime):
		return status.Error(codes.FailedPrecondition, "source or runtime is unavailable")
	}
	return status.Error(codes.Internal, message)
}
func Serve(provider Provider) {
	goplugin.Serve(&goplugin.ServeConfig{HandshakeConfig: Handshake(), Plugins: goplugin.PluginSet{PluginKey: &GRPCPlugin{Provider: provider}}, GRPCServer: func(options []grpc.ServerOption) *grpc.Server {
		return grpc.NewServer(append(options, grpc.MaxRecvMsgSize(MaxMessageBytes), grpc.MaxSendMsgSize(MaxMessageBytes), grpc.MaxConcurrentStreams(16))...)
	}, Logger: hclog.NewNullLogger()})
}
