//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package pluginprocess

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// Portable development tests use the caller's credentials in a new process
// group. Production Windows credential restrictions are tested natively.
type processControl struct{}

func prepareProcessProfile(ctx context.Context, cmd *exec.Cmd, _ bool) (*processControl, error) {
	return prepareProcess(ctx, cmd)
}

func prepareProcess(_ context.Context, cmd *exec.Cmd) (*processControl, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return &processControl{}, nil
}
func (*processControl) start(cmd *exec.Cmd) error { return cmd.Start() }
func (*processControl) wait(cmd *exec.Cmd) (uint32, error) {
	err := cmd.Wait()
	if cmd.ProcessState == nil {
		return 0, err
	}
	return uint32(cmd.ProcessState.ExitCode()), err
}
func (*processControl) started(*exec.Cmd) error { return nil }
func (*processControl) kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
func (*processControl) close() {}
func openExecutable(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&0o022 != 0 {
		return nil, errors.New("executable must be a regular file without group/other write access")
	}
	return os.Open(path)
}
func environment() []string { return nil }
