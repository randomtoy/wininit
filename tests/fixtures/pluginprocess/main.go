// A real subprocess fixture for negotiation, deadlines, crash, and redaction tests.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"
	datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fixture struct {
	datasourcev1.UnimplementedDatasourceServer
	mode string
}

func (f *fixture) Describe(context.Context, *datasourcev1.DescribeRequest) (*datasourcev1.Identity, error) {
	name := datasourcev1.PluginKey
	if f.mode == "identity" {
		name = "wrong"
	}
	return &datasourcev1.Identity{ProtocolVersion: 1, Name: name, Kind: "datasource"}, nil
}
func (f *fixture) Discover(ctx context.Context, request *datasourcev1.DiscoverRequest) (*datasourcev1.DiscoverResponse, error) {
	switch f.mode {
	case "restrictions":
		if err := checkRestrictions(request.GetSeedDirectory()); err != nil {
			// This dedicated test mode returns only controlled assertion text,
			// so a native failure identifies the exact restriction. Production
			// providers never encode errors as metadata.
			return &datasourcev1.DiscoverResponse{Metadata: &datasourcev1.Metadata{InstanceId: "restriction-failed: " + err.Error()}}, nil
		}
	case "hang":
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	case "crash":
		os.Exit(23)
	case "leak":
		return nil, status.Error(codes.Internal, "fixture-private-secret")
	case "invalid":
		return &datasourcev1.DiscoverResponse{Metadata: &datasourcev1.Metadata{InstanceId: "", LocalHostname: "bad/path"}}, nil
	case "unnormalized":
		return &datasourcev1.DiscoverResponse{Metadata: &datasourcev1.Metadata{InstanceId: "vm", LocalHostname: "Node.example"}}, nil
	case "earlydeadline":
		return nil, status.Error(codes.DeadlineExceeded, "fixture-private-secret")
	case "unavailable":
		return &datasourcev1.DiscoverResponse{Unavailable: true}, nil
	case "conflict":
		return &datasourcev1.DiscoverResponse{Unavailable: true, Metadata: &datasourcev1.Metadata{InstanceId: "vm"}}, nil
	case "environment":
		if os.Getenv("WININIT_TEST_SECRET") != "" {
			return nil, status.Error(codes.Internal, "inherited environment")
		}
	}
	return &datasourcev1.DiscoverResponse{Metadata: &datasourcev1.Metadata{InstanceId: "fixture-instance", LocalHostname: "fixture-node"}}, nil
}

type fixturePlugin struct {
	datasourcev1.GRPCPlugin
	server *fixture
}

func (p *fixturePlugin) GRPCServer(_ *goplugin.GRPCBroker, server *grpc.Server) error {
	datasourcev1.RegisterDatasourceServer(server, p.server)
	return nil
}
func main() {
	if strings.TrimSuffix(filepath.Base(os.Args[0]), filepath.Ext(os.Args[0])) == "native-exit23" {
		os.Exit(23)
	}
	mode := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if strings.HasPrefix(mode, "prov-") {
		serveProvisionerFixture(mode)
		return
	}
	if strings.HasPrefix(mode, "ps-") {
		servePowerShellFixture(mode)
		return
	}
	if mode == "bootstrap" {
		fmt.Println("ready")
		time.Sleep(time.Hour)
		return
	}
	if mode == "nohandshake" {
		fmt.Println("fixture-private-secret")
		time.Sleep(time.Hour)
		return
	}
	if mode == "silent" {
		time.Sleep(time.Hour)
		return
	}
	handshake := datasourcev1.Handshake()
	if mode == "version" {
		handshake.ProtocolVersion = 99
	}
	goplugin.Serve(&goplugin.ServeConfig{HandshakeConfig: handshake, Plugins: goplugin.PluginSet{datasourcev1.PluginKey: &fixturePlugin{server: &fixture{mode: mode}}}, GRPCServer: goplugin.DefaultGRPCServer, Logger: hclog.NewNullLogger()})
}
