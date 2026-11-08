package integration

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDraftReleaseDeclaresRepositoryOutsideGitCheckout(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		Jobs map[string]struct {
			Steps []struct {
				Run string
				Env map[string]string
			}
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range workflow.Jobs["publish"].Steps {
		if !strings.Contains(step.Run, "gh release create") {
			continue
		}
		found = true
		if step.Env["GH_REPO"] != "${{ github.repository }}" {
			t.Fatal("draft publisher needs an explicit repository: its artifact-only job has no Git checkout")
		}
		if !strings.Contains(step.Run, "--draft") || !strings.Contains(step.Run, "--prerelease") {
			t.Fatal("release preparation must create a draft prerelease")
		}
	}
	if !found {
		t.Fatal("draft publisher step is missing")
	}
}

func TestGuestWorkflowTargetsOnlyReviewedMainRunnerGroup(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", ".github", "workflows", "e2e.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var workflow struct {
		On   map[string]any
		Jobs map[string]struct {
			If     string
			RunsOn struct{ Group string } `yaml:"runs-on"`
		}
	}
	if err := yaml.Unmarshal(data, &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On) != 1 {
		t.Fatal("privileged guest workflow must only be manually dispatched")
	}
	if _, exists := workflow.On["workflow_dispatch"]; !exists {
		t.Fatal("guest dispatch trigger is missing")
	}
	guest := workflow.Jobs["guest"]
	if guest.RunsOn.Group != "wininit-e2e-reviewed" || guest.If != "github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main'" {
		t.Fatal("privileged guest jobs require reviewed main and the workflow-restricted runner group")
	}
}
