package main

import (
	"context"
	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"strings"
)

type psFixture struct {
	userdatav1.UnimplementedUserDataServer
	mode string
}

func (f *psFixture) Describe(context.Context, *userdatav1.DescribeRequest) (*userdatav1.Identity, error) {
	name := userdatav1.PluginKey
	if f.mode == "ps-identity" {
		name = "wrong"
	}
	return &userdatav1.Identity{ProtocolVersion: 1, Name: name, Kind: "provisioner"}, nil
}
func (f *psFixture) Prepare(_ context.Context, r *userdatav1.PrepareRequest) (*userdatav1.Prepared, error) {
	switch f.mode {
	case "ps-leak":
		return nil, status.Error(codes.Internal, "fixture-private-secret")
	case "ps-environment":
		if os.Getenv("WININIT_TEST_SECRET") != "" {
			return nil, status.Error(codes.Internal, "fixture-private-secret")
		}
	case "ps-invalid":
		return &userdatav1.Prepared{LeaseId: "bad", Sha256: r.ApprovedSha256, Size: 100}, nil
	case "ps-conflict":
		return &userdatav1.Prepared{Unavailable: true, LeaseId: "unexpected"}, nil
	case "ps-unavailable":
		return &userdatav1.Prepared{Unavailable: true}, nil
	}
	return &userdatav1.Prepared{LeaseId: strings.Repeat("a", 32), Sha256: r.ApprovedSha256, Size: 100}, nil
}
func (f *psFixture) Execute(ctx context.Context, r *userdatav1.ExecuteRequest) (*userdatav1.Execution, error) {
	switch f.mode {
	case "ps-hang":
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	case "ps-error":
		return nil, status.Error(codes.Internal, "fixture-private-secret")
	case "ps-failed":
		return &userdatav1.Execution{Outcome: userdatav1.Outcome_EXIT_FAILURE, ExitCode: 17}, nil
	case "ps-outputlimit":
		return &userdatav1.Execution{Outcome: userdatav1.Outcome_OUTPUT_LIMIT, ExitCode: 1, OutputBytes: uint64(r.MaxOutputBytes) + 1}, nil
	case "ps-oversized":
		return &userdatav1.Execution{Outcome: userdatav1.Outcome_SUCCESS, OutputBytes: uint64(r.MaxOutputBytes) + 2}, nil
	case "ps-overflow-success":
		return &userdatav1.Execution{Outcome: userdatav1.Outcome_SUCCESS, OutputBytes: uint64(r.MaxOutputBytes) + 1}, nil
	case "ps-conflicting-success":
		return &userdatav1.Execution{Outcome: userdatav1.Outcome_SUCCESS, ExitCode: 7}, nil
	case "ps-reboot":
		return &userdatav1.Execution{Outcome: userdatav1.Outcome_REBOOT_REQUIRED, ExitCode: 3010}, nil
	}
	return &userdatav1.Execution{Outcome: userdatav1.Outcome_SUCCESS}, nil
}

type psFixturePlugin struct {
	userdatav1.GRPCPlugin
	server *psFixture
}

func (p *psFixturePlugin) GRPCServer(_ *goplugin.GRPCBroker, server *grpc.Server) error {
	userdatav1.RegisterUserDataServer(server, p.server)
	return nil
}
func servePowerShellFixture(mode string) {
	handshake := userdatav1.Handshake()
	if mode == "ps-version" {
		handshake.ProtocolVersion = 99
	}
	goplugin.Serve(&goplugin.ServeConfig{HandshakeConfig: handshake, Plugins: goplugin.PluginSet{userdatav1.PluginKey: &psFixturePlugin{server: &psFixture{mode: mode}}}, GRPCServer: goplugin.DefaultGRPCServer, Logger: hclog.NewNullLogger()})
}
