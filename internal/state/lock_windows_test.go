//go:build windows

package state

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const abandonHelperEnvironment = "WININIT_TEST_ABANDON_MACHINE_LOCK"

const (
	namespaceLifetimeRoleEnvironment     = "WININIT_TEST_NAMESPACE_LIFETIME_ROLE"
	namespaceLifetimeAliasEnvironment    = "WININIT_TEST_NAMESPACE_LIFETIME_ALIAS"
	namespaceLifetimeBoundaryEnvironment = "WININIT_TEST_NAMESPACE_LIFETIME_BOUNDARY"
)

func TestPrivateNamespaceSurvivesCreatorExitWithoutSplitLock(t *testing.T) {
	requireElevated(t)

	suffix := fmt.Sprintf("%d_%d", os.Getpid(), time.Now().UnixNano())
	environment := append(os.Environ(),
		namespaceLifetimeAliasEnvironment+"=WininitPrivateTest_"+suffix,
		namespaceLifetimeBoundaryEnvironment+"=WininitBoundaryTest_"+suffix,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	creator, creatorInput := startNamespaceLifetimeHelper(t, ctx, environment, "creator")
	owner, ownerInput := startNamespaceLifetimeHelper(t, ctx, environment, "owner")
	if err := creatorInput.Close(); err != nil {
		t.Fatal(err)
	}
	if err := creator.Wait(); err != nil {
		t.Fatalf("namespace creator failed: %v", err)
	}

	contender := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrivateNamespaceLifetimeHelper$")
	contender.Env = append(environment, namespaceLifetimeRoleEnvironment+"=contender")
	output, err := contender.CombinedOutput()
	if err != nil {
		_ = ownerInput.Close()
		_ = owner.Wait()
		t.Fatalf("namespace contender was not excluded after creator exit: %v, output=%q", err, output)
	}
	if strings.TrimSpace(string(output)) != "blocked" {
		_ = ownerInput.Close()
		_ = owner.Wait()
		t.Fatalf("unexpected namespace contender output %q", output)
	}

	if err := ownerInput.Close(); err != nil {
		t.Fatal(err)
	}
	if err := owner.Wait(); err != nil {
		t.Fatalf("namespace owner failed: %v", err)
	}
}

func TestPrivateNamespaceLifetimeHelper(t *testing.T) {
	role := os.Getenv(namespaceLifetimeRoleEnvironment)
	if role == "" {
		return
	}
	alias := os.Getenv(namespaceLifetimeAliasEnvironment)
	boundary := os.Getenv(namespaceLifetimeBoundaryEnvironment)
	if alias == "" || boundary == "" {
		t.Fatal("namespace lifetime helper names are missing")
	}
	if _, err := createOrOpenPrivateNamespace(alias, boundary); err != nil {
		t.Fatal(err)
	}

	switch role {
	case "creator":
		fmt.Fprintln(os.Stdout, "ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
	case "owner":
		result := make(chan machineLockResult, 1)
		go ownMachineLock(context.Background(), alias+`\Provisioning`, result)
		acquired := <-result
		if acquired.err != nil {
			t.Fatal(acquired.err)
		}
		fmt.Fprintln(os.Stdout, "ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		acquired.release()
	case "contender":
		result := make(chan machineLockResult, 1)
		go ownMachineLock(context.Background(), alias+`\Provisioning`, result)
		acquired := <-result
		if acquired.err == nil {
			acquired.release()
			t.Fatal("acquired a split lock after the namespace creator exited")
		}
		if !strings.Contains(acquired.err.Error(), "another wininit lifecycle process") {
			t.Fatalf("contender failed for an unexpected reason: %v", acquired.err)
		}
		fmt.Fprintln(os.Stdout, "blocked")
	default:
		t.Fatalf("unknown namespace lifetime helper role %q", role)
	}
}

func TestMachineLockExcludesSecondLifecycleAndReleasesAcrossGoroutines(t *testing.T) {
	requireElevated(t)
	release, err := AcquireMachineLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second, err := AcquireMachineLock(context.Background()); err == nil {
		second()
		release()
		t.Fatal("concurrent lifecycle acquired machine lock")
	}

	released := make(chan struct{})
	go func() {
		release()
		release()
		close(released)
	}()
	select {
	case <-released:
	case <-time.After(5 * time.Second):
		t.Fatal("cross-goroutine lock release did not complete")
	}
	assertMutexHasNoHandle(t)

	release, err = AcquireMachineLock(context.Background())
	if err != nil {
		t.Fatalf("released lock cannot be acquired: %v", err)
	}
	release()
}

func TestMachineLockUsesProtectedSystemAdministratorsDACL(t *testing.T) {
	requireElevated(t)
	release, err := AcquireMachineLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	handle := openMachineMutex(t, windows.READ_CONTROL)
	defer windows.CloseHandle(handle)
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("read mutex security descriptor: %v", err)
	}
	control, _, err := descriptor.Control()
	if err != nil {
		t.Fatalf("read mutex security descriptor control: %v", err)
	}
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatal("mutex DACL inherits permissions")
	}
	sddl := descriptor.String()
	for _, ace := range []string{"(A;;GA;;;SY)", "(A;;GA;;;BA)"} {
		if !strings.Contains(sddl, ace) {
			t.Fatalf("mutex security descriptor %q does not contain %s", sddl, ace)
		}
	}
}

func TestMachineLockRecoversAbandonedOwnership(t *testing.T) {
	requireElevated(t)
	if err := ensurePrivateNamespace(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMachineLockAbandonHelper$")
	command.Env = append(os.Environ(), abandonHelperEnvironment+"=1")
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		_ = stdin.Close()
		_ = command.Wait()
		t.Fatalf("abandon helper did not acquire lock: stdout=%q stderr=%q", scanner.Text(), stderr.String())
	}
	keeper := openMachineMutex(t, windows.SYNCHRONIZE)
	if err := stdin.Close(); err != nil {
		windows.CloseHandle(keeper)
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		windows.CloseHandle(keeper)
		t.Fatalf("abandon helper failed: %v, stderr=%q", err, stderr.String())
	}

	release, err := AcquireMachineLock(context.Background())
	_ = windows.CloseHandle(keeper)
	if err != nil {
		t.Fatalf("abandoned lock was not recoverable: %v", err)
	}
	release()
}

func TestMachineLockAbandonHelper(t *testing.T) {
	if os.Getenv(abandonHelperEnvironment) != "1" {
		return
	}
	release, err := AcquireMachineLock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_ = release // The process deliberately exits without releasing ownership.
	fmt.Fprintln(os.Stdout, "ready")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func startNamespaceLifetimeHelper(t *testing.T, ctx context.Context, environment []string, role string) (*exec.Cmd, io.WriteCloser) {
	t.Helper()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPrivateNamespaceLifetimeHelper$")
	command.Env = append(environment, namespaceLifetimeRoleEnvironment+"="+role)
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		_ = stdin.Close()
		_ = command.Wait()
		t.Fatalf("namespace %s helper did not become ready: stdout=%q stderr=%q", role, scanner.Text(), stderr.String())
	}
	return command, stdin
}

func requireElevated(t *testing.T) {
	t.Helper()
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("private namespace integration requires an elevated Windows token")
	}
}

func openMachineMutex(t *testing.T, access uint32) windows.Handle {
	t.Helper()
	name, err := windows.UTF16PtrFromString(machineMutexName)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenMutex(access, false, name)
	if err != nil {
		t.Fatalf("open lifecycle mutex: %v", err)
	}
	return handle
}

func assertMutexHasNoHandle(t *testing.T) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(machineMutexName)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, name)
	if handle != 0 {
		windows.CloseHandle(handle)
		t.Fatal("lifecycle mutex still has an owner handle after release")
	}
	if err == nil {
		t.Fatal("opening released lifecycle mutex unexpectedly succeeded")
	}
}
