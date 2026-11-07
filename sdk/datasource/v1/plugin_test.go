package datasourcev1

import (
	"context"
	"testing"
)

func TestNamedIdentityPreservesOriginalConfigDriveDefault(t *testing.T) {
	for _, tc := range []struct{ name, want string }{{"", "configdrive"}, {"nocloud", "nocloud"}} {
		s := &providerServer{name: tc.name}
		id, err := s.Describe(context.Background(), nil)
		if err != nil || id.Name != tc.want || id.Kind != "datasource" || id.ProtocolVersion != 1 {
			t.Fatal(id, err)
		}
	}
}
func TestServeNamedRejectsInvalidRegistration(t *testing.T) {
	if err := ServeNamed("bad/name", nil); err == nil {
		t.Fatal("invalid name accepted")
	}
	if err := ServeNamed("nocloud", nil); err == nil {
		t.Fatal("nil provider accepted")
	}
}
