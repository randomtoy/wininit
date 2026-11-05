package version

import "testing"

func TestCurrent(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, Commit, BuildDate
	t.Cleanup(func() { Version, Commit, BuildDate = originalVersion, originalCommit, originalDate })
	Version, Commit, BuildDate = "v1.2.3", "abc123", "2026-11-05T00:00:00Z"
	got := Current()
	if got.Version != Version || got.Commit != Commit || got.BuildDate != BuildDate || got.GoVersion == "" || got.OS == "" || got.Arch == "" {
		t.Fatalf("Current() = %#v", got)
	}
}
