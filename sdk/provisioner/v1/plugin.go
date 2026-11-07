// Package provisionerv1 is the experimental metadata-only process SDK.
// It depends on no wininit internal packages or lifecycle/state interfaces.
package provisionerv1

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const ProtocolVersion = 1
const PluginKey = "provisioner"
const MaxMessageBytes = 128 << 10

var ErrInvalidMetadata = errors.New("invalid provisioner metadata")

func Handshake() goplugin.HandshakeConfig {
	return goplugin.HandshakeConfig{ProtocolVersion: ProtocolVersion, MagicCookieKey: "WININIT_PLUGIN_PROTOCOL", MagicCookieValue: "wininit-provisioner-v1"}
}
func ValidName(name string) bool {
	return regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`).MatchString(name)
}
func ValidateMetadata(value *Metadata) error {
	if value == nil || !utf8.ValidString(value.GetInstanceId()) || !utf8.ValidString(value.GetLocalHostname()) || strings.TrimSpace(value.InstanceId) == "" || len(value.InstanceId) > 256 || len(value.LocalHostname) > 256 || strings.IndexFunc(value.InstanceId, unicode.IsControl) >= 0 || strings.IndexFunc(value.LocalHostname, unicode.IsControl) >= 0 || (value.Datasource != "nocloud" && value.Datasource != "configdrive") {
		return ErrInvalidMetadata
	}
	return nil
}
func ValidOutcome(outcome Outcome) bool {
	return outcome >= Outcome_UNCHANGED && outcome <= Outcome_SKIPPED
}

// Provider must reconcile the same desired metadata safely on every agent start.
// One-shot work belongs to userdata v1 and its durable execution journal.
type Provider interface {
	Apply(context.Context, *ApplyRequest) (*ApplyResponse, error)
}
type GRPCPlugin struct {
	goplugin.NetRPCUnsupportedPlugin
	Name     string
	Provider Provider
}

func (p *GRPCPlugin) GRPCServer(_ *goplugin.GRPCBroker, server *grpc.Server) error {
	if !ValidName(p.Name) || p.Provider == nil {
		return errors.New("invalid provisioner identity or provider")
	}
	RegisterProvisionerServer(server, &providerServer{name: p.Name, provider: p.Provider})
	return nil
}
func (*GRPCPlugin) GRPCClient(_ context.Context, _ *goplugin.GRPCBroker, connection *grpc.ClientConn) (interface{}, error) {
	return NewProvisionerClient(connection), nil
}

type providerServer struct {
	UnimplementedProvisionerServer
	name     string
	provider Provider
}

func (s *providerServer) Describe(context.Context, *DescribeRequest) (*Identity, error) {
	return &Identity{ProtocolVersion: ProtocolVersion, Name: s.name, Kind: "provisioner", Idempotent: true}, nil
}
func (s *providerServer) Apply(ctx context.Context, request *ApplyRequest) (*ApplyResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := ValidateMetadata(request.GetMetadata()); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid metadata")
	}
	if s.provider == nil {
		return nil, status.Error(codes.FailedPrecondition, "provider is not configured")
	}
	result, err := s.provider.Apply(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, status.FromContextError(ctx.Err()).Err()
		}
		return nil, status.Error(codes.Internal, "provisioner reconciliation failed")
	}
	if result == nil || !ValidOutcome(result.Outcome) {
		return nil, status.Error(codes.Internal, "invalid provisioner outcome")
	}
	return result, nil
}
func Serve(name string, provider Provider) error {
	if !ValidName(name) || provider == nil {
		return errors.New("invalid provisioner identity or provider")
	}
	goplugin.Serve(&goplugin.ServeConfig{HandshakeConfig: Handshake(), Plugins: goplugin.PluginSet{PluginKey: &GRPCPlugin{Name: name, Provider: provider}}, GRPCServer: func(options []grpc.ServerOption) *grpc.Server {
		return grpc.NewServer(append(options, grpc.MaxRecvMsgSize(MaxMessageBytes), grpc.MaxSendMsgSize(MaxMessageBytes), grpc.MaxConcurrentStreams(16))...)
	}, Logger: hclog.NewNullLogger()})
	return nil
}
