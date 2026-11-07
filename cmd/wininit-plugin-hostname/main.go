package main

import (
	platformhostname "github.com/randomtoy/wininit/internal/platform/hostname"
	"github.com/randomtoy/wininit/plugins/provisioner/hostnameprocess"
	provisionerv1 "github.com/randomtoy/wininit/sdk/provisioner/v1"
	"os"
)

func main() {
	if err := provisionerv1.Serve("hostname", hostnameprocess.Provider{Platform: platformhostname.New()}); err != nil {
		os.Exit(1)
	}
}
