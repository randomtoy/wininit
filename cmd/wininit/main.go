package main

import (
	"fmt"
	"os"

	"github.com/randomtoy/wininit/internal/app"
	"github.com/randomtoy/wininit/internal/service"
)

func main() {
	isService, err := service.IsWindowsService()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: detect Windows service context: %v\n", err)
		os.Exit(1)
	}
	if isService {
		if err := app.RunService(); err != nil {
			fmt.Fprintf(os.Stderr, "error: run Windows service: %v\n", err)
			os.Exit(1)
		}
		return
	}
	ctx, cancel := app.SignalContext()
	defer cancel()
	os.Exit(app.Execute(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
