package provisionerv1

import (
	"context"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strings"
	"testing"
)

type providerFunc func(context.Context, *ApplyRequest) (*ApplyResponse, error)

func (f providerFunc) Apply(ctx context.Context, r *ApplyRequest) (*ApplyResponse, error) {
	return f(ctx, r)
}
func TestProviderBoundaryValidatesBeforeDispatchAndRedactsFailures(t *testing.T) {
	called := 0
	s := &providerServer{name: "hostname", provider: providerFunc(func(context.Context, *ApplyRequest) (*ApplyResponse, error) {
		called++
		return nil, errors.New("fixture-private-secret")
	})}
	for _, metadata := range []*Metadata{nil, {}, {InstanceId: "vm", Datasource: "unknown"}, {InstanceId: "vm\n", Datasource: "nocloud"}, {InstanceId: "vm", LocalHostname: strings.Repeat("x", 257), Datasource: "nocloud"}} {
		if _, err := s.Apply(context.Background(), &ApplyRequest{Metadata: metadata}); status.Code(err) != codes.InvalidArgument {
			t.Fatal(err)
		}
	}
	if called != 0 {
		t.Fatal("invalid metadata reached provider")
	}
	request := &ApplyRequest{Metadata: &Metadata{InstanceId: "vm", Datasource: "nocloud"}}
	if _, err := s.Apply(context.Background(), request); status.Code(err) != codes.Internal || strings.Contains(err.Error(), "fixture-private-secret") {
		t.Fatal(err)
	}
	for _, result := range []*ApplyResponse{nil, {}, {Outcome: 99}} {
		s.provider = providerFunc(func(context.Context, *ApplyRequest) (*ApplyResponse, error) { return result, nil })
		if _, err := s.Apply(context.Background(), request); status.Code(err) != codes.Internal {
			t.Fatal("invalid outcome accepted", err)
		}
	}
	s.provider = providerFunc(func(ctx context.Context, _ *ApplyRequest) (*ApplyResponse, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if _, err := s.Apply(ctx, request); status.Code(err) != codes.Canceled {
		t.Fatal(err)
	}
}
func TestIdentityAndServeValidation(t *testing.T) {
	s := &providerServer{name: "hostname"}
	id, err := s.Describe(context.Background(), nil)
	if err != nil || id.ProtocolVersion != 1 || id.Kind != "provisioner" || id.Name != "hostname" || !id.Idempotent {
		t.Fatal(id, err)
	}
	if err := Serve("bad/name", providerFunc(nil)); err == nil {
		t.Fatal("invalid plugin identity accepted")
	}
	if err := Serve("hostname", nil); err == nil {
		t.Fatal("nil provider accepted")
	}
}
