// Package version exposes build metadata populated with Go linker flags.
package version

import "runtime"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func Current() Info {
	return Info{Version: Version, Commit: Commit, BuildDate: BuildDate, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH}
}
