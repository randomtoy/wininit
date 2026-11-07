package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// Build outside this module: merely compiling inside the repository would not
// prove that external consumers can use the SDK without internal-package access.
func TestSDKBuildsFromIndependentConsumerModule(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("missing source location")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	module := "module example.org/wininit-sdk-consumer\n\ngo 1.27.0\n\nrequire github.com/randomtoy/wininit v0.0.0\nreplace github.com/randomtoy/wininit => " + strconv.Quote(filepath.ToSlash(root)) + "\n"
	source := `package main
import (
 "context"
 datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
 provisionerv1 "github.com/randomtoy/wininit/sdk/provisioner/v1"
 userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
)
type datasource struct{}
func(datasource)Discover(context.Context,*datasourcev1.DiscoverRequest)(*datasourcev1.DiscoverResponse,error){return &datasourcev1.DiscoverResponse{Unavailable:true},nil}
type provisioner struct{}
func(provisioner)Apply(context.Context,*provisionerv1.ApplyRequest)(*provisionerv1.ApplyResponse,error){return &provisionerv1.ApplyResponse{Outcome:provisionerv1.Outcome_UNCHANGED},nil}
var _ datasourcev1.Provider=datasource{}
var _ provisionerv1.Provider=provisioner{}
func main(){_ = datasourcev1.Handshake();_ = provisionerv1.Handshake();_ = userdatav1.Handshake()}
`
	for name, data := range map[string]string{"go.mod": module, "main.go": source} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "build", "-mod=mod", ".")
	cmd.Dir = directory
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("external SDK consumer failed: %v\n%s", err, output)
	}
}
