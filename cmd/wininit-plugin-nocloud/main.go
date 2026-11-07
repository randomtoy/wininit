package main

import (
	"github.com/randomtoy/wininit/plugins/datasource/nocloudprocess"
	datasourcev1 "github.com/randomtoy/wininit/sdk/datasource/v1"
	"os"
)

func main() {
	if err := datasourcev1.ServeNamed("nocloud", nocloudprocess.Provider{}); err != nil {
		os.Exit(1)
	}
}
