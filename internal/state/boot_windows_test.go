//go:build windows

package state

import (
	"context"
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestRegistryBootMarkerSurvivesStoreRecreationAndFailsClosed(t *testing.T) {
	path := fmt.Sprintf(`SOFTWARE\WininitTests\boot-%d`, time.Now().UnixNano())
	store := newRegistryStore(registry.LOCAL_MACHINE, path)
	t.Cleanup(func() {
		_ = registry.DeleteKey(registry.LOCAL_MACHINE, path+`\BootSession`)
		_ = registry.DeleteKey(registry.LOCAL_MACHINE, path)
	})
	ctx := context.Background()
	if err := store.Save(ctx, Initial(time.Now())); err != nil {
		t.Fatal(err)
	}
	first, err := store.BootID(ctx)
	if err != nil || !ValidHash(first) {
		t.Fatalf("boot identity=%q error=%v", first, err)
	}
	second, err := newRegistryStore(registry.LOCAL_MACHINE, path).BootID(ctx)
	if err != nil || first != second {
		t.Fatal("new process/store lost boot identity", err)
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, path+`\BootSession`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.DeleteValue("ID"); err != nil {
		key.Close()
		t.Fatal(err)
	}
	key.Close()
	if _, err := store.BootID(ctx); err == nil {
		t.Fatal("corrupt boot marker silently invented a new boot")
	}
}
