package main

import (
	"github.com/randomtoy/wininit/plugins/provisioner/powershell"
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
)

func main() { userdatav1.Serve(powershell.New(powershell.NativeExecutor{})) }
