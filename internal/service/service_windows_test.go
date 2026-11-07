//go:build windows

package service

import (
	"context"
	"errors"
	"golang.org/x/sys/windows/svc"
	"strings"
	"testing"
	"time"
)

type statusSequence struct {
	values []svc.Status
	calls  int
}

func (s *statusSequence) Query() (svc.Status, error) {
	index := s.calls
	s.calls++
	if index >= len(s.values) {
		index = len(s.values) - 1
	}
	return s.values[index], nil
}
func TestStartWaitReportsEarlyServiceFailureInsteadOfWaitingForCancellation(t *testing.T) {
	sequence := &statusSequence{values: []svc.Status{{State: svc.StartPending}, {State: svc.Stopped, Win32ExitCode: 1066, ServiceSpecificExitCode: 1}}}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	start := time.Now()
	err := waitForState(ctx, sequence, svc.Running)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "Windows exit code 1066, service exit code 1") || time.Since(start) > time.Second {
		t.Fatalf("startup failure hidden: %v calls=%d", err, sequence.calls)
	}
}
func TestServiceWaitAcceptsStoppedCleanupAndHonorsCancellation(t *testing.T) {
	if err := waitForState(context.Background(), &statusSequence{values: []svc.Status{{State: svc.Stopped, Win32ExitCode: 1}}}, svc.Stopped); err != nil {
		t.Fatal(err)
	}
	ctx, stop := context.WithCancel(context.Background())
	stop()
	if err := waitForState(ctx, &statusSequence{values: []svc.Status{{State: svc.StartPending}}}, svc.Running); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
