package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestVersionCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"version", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("Execute() code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"version"`) || !strings.Contains(stdout.String(), `"go_version"`) {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("Execute() code = %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
