package nocloudprocess

import (
	"context"
	datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderReadsOnlyMetadataAndReportsAbsence(t *testing.T) {
	dir := t.TempDir()
	request := &datasourcev1.DiscoverRequest{SeedDirectory: dir}
	value, err := (Provider{}).Discover(context.Background(), request)
	if err != nil || !value.GetUnavailable() {
		t.Fatal(value, err)
	}
	for name, data := range map[string]string{"meta-data": "instance-id: vm\nlocal-hostname: NODE\n", "user-data": "#ps1\nthrow 'must never execute'"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	value, err = (Provider{}).Discover(context.Background(), request)
	if err != nil || value.GetMetadata().GetInstanceId() != "vm" || value.GetMetadata().GetLocalHostname() != "NODE" {
		t.Fatal(value, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta-data"), []byte("instance-id: [invalid]"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Provider{}).Discover(context.Background(), request); err == nil {
		t.Fatal("malformed metadata treated as absence")
	}
}
