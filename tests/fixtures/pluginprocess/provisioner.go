package main

import (
	"context"
	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	provisionerv1 "github.com/randomtoy/wininit/sdk/provisioner/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"os"
	"time"
)

type provisionerFixture struct {
	provisionerv1.UnimplementedProvisionerServer
	mode string
}

func (f *provisionerFixture) Describe(ctx context.Context, _ *provisionerv1.DescribeRequest) (*provisionerv1.Identity, error) {
	if f.mode == "prov-describe-hang" {
		if err := os.WriteFile(os.Args[0]+".started", []byte("describe"), 0600); err != nil {
			return nil, err
		}
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	}
	name, kind, version, idempotent := "hostname", "provisioner", uint32(1), true
	switch f.mode {
	case "prov-identity":
		name = "wrong"
	case "prov-kind":
		kind = "datasource"
	case "prov-wire-version":
		version = 99
	case "prov-once":
		idempotent = false
	}
	return &provisionerv1.Identity{Name: name, Kind: kind, ProtocolVersion: version, Idempotent: idempotent}, nil
}
func (f *provisionerFixture) Apply(ctx context.Context, _ *provisionerv1.ApplyRequest) (*provisionerv1.ApplyResponse, error) {
	switch f.mode {
	case "prov-hang":
		if err := os.WriteFile(os.Args[0]+".started", []byte("apply"), 0600); err != nil {
			return nil, err
		}
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	case "prov-earlydeadline":
		return nil, status.Error(codes.DeadlineExceeded, "fixture-private-secret")
	case "prov-error":
		return nil, status.Error(codes.Internal, "fixture-private-secret")
	case "prov-crash":
		os.Exit(23)
	case "prov-environment":
		if os.Getenv("WININIT_TEST_SECRET") != "" {
			return nil, status.Error(codes.Internal, "fixture-private-secret")
		}
	case "prov-invalid":
		return &provisionerv1.ApplyResponse{}, nil
	case "prov-unknown":
		return &provisionerv1.ApplyResponse{Outcome: 99}, nil
	case "prov-changed":
		return &provisionerv1.ApplyResponse{Outcome: provisionerv1.Outcome_CHANGED}, nil
	case "prov-reboot":
		return &provisionerv1.ApplyResponse{Outcome: provisionerv1.Outcome_REBOOT_REQUIRED}, nil
	case "prov-changed-reboot":
		return &provisionerv1.ApplyResponse{Outcome: provisionerv1.Outcome_CHANGED_REBOOT_REQUIRED}, nil
	case "prov-skipped":
		return &provisionerv1.ApplyResponse{Outcome: provisionerv1.Outcome_SKIPPED}, nil
	case "prov-restrictions":
		if err := checkProvisionerRestrictions(); err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}
	return &provisionerv1.ApplyResponse{Outcome: provisionerv1.Outcome_UNCHANGED}, nil
}

type provisionerFixturePlugin struct {
	provisionerv1.GRPCPlugin
	server *provisionerFixture
}

func (p *provisionerFixturePlugin) GRPCServer(_ *goplugin.GRPCBroker, server *grpc.Server) error {
	provisionerv1.RegisterProvisionerServer(server, p.server)
	return nil
}
func serveProvisionerFixture(mode string) {
	if mode == "prov-nohandshake" {
		if err := os.WriteFile(os.Args[0]+".started", []byte("handshake"), 0600); err != nil {
			os.Exit(1)
		}
		os.Stdout.WriteString("fixture-private-secret")
		time.Sleep(time.Hour)
		return
	}
	handshake := provisionerv1.Handshake()
	if mode == "prov-version" {
		handshake.ProtocolVersion = 99
	}
	goplugin.Serve(&goplugin.ServeConfig{HandshakeConfig: handshake, Plugins: goplugin.PluginSet{provisionerv1.PluginKey: &provisionerFixturePlugin{server: &provisionerFixture{mode: mode}}}, GRPCServer: goplugin.DefaultGRPCServer, Logger: hclog.NewNullLogger()})
}
