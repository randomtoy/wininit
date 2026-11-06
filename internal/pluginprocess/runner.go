package pluginprocess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type processRunner struct {
	cmd            *exec.Cmd
	stdout, stderr io.ReadCloser
	control        *processControl
	directory      string
	cancelled      atomic.Bool
	once           sync.Once
	startFailure   error
	exitStatus     atomic.Uint64
	finished       chan struct{}
}

func newProcessRunner(cmd *exec.Cmd, directory string) (*processRunner, error) {
	control, err := prepareProcess(cmd)
	if err != nil {
		return nil, err
	}
	r := &processRunner{cmd: cmd, control: control, directory: directory, finished: make(chan struct{})}
	r.stdout, err = cmd.StdoutPipe()
	if err != nil {
		control.close()
		return nil, err
	}
	r.stderr, err = cmd.StderrPipe()
	if err != nil {
		r.stdout.Close()
		control.close()
		return nil, err
	}
	cmd.Cancel = func() error {
		r.cancelled.Store(true)
		err := r.control.kill(cmd)
		if err != nil {
			r.cancelled.Store(false)
		}
		return err
	}
	cmd.WaitDelay = pipeDrainTimeout
	return r, nil
}

const pipeDrainTimeout = 2 * time.Second

func (r *processRunner) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		r.close()
		return err
	}
	if err := r.cmd.Start(); err != nil {
		r.startFailure = processError("process creation", err)
		r.close()
		return err
	}
	if err := r.control.started(r.cmd); err != nil {
		r.startFailure = processError("process containment", err)
		r.cmd.Process.Kill()
		r.cmd.Wait()
		r.close()
		return err
	}
	return nil
}

// Expose only host operation names and numeric OS codes, never child payloads.
func processError(operation string, cause error) error {
	var code syscall.Errno
	if errors.As(cause, &code) {
		return fmt.Errorf("external ConfigDrive %s failed (OS code %d)", operation, uint32(code))
	}
	return fmt.Errorf("external ConfigDrive %s failed", operation)
}
func (r *processRunner) close() { r.once.Do(func() { r.control.close() }) }
func (r *processRunner) Wait(context.Context) error {
	defer r.close()
	defer close(r.finished)
	err := r.cmd.Wait()
	if r.cmd.ProcessState != nil {
		r.exitStatus.Store(1<<32 | uint64(uint32(r.cmd.ProcessState.ExitCode())))
	}
	return err
}
func (r *processRunner) Kill(context.Context) error {
	err := r.control.kill(r.cmd)
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
func (r *processRunner) Stdout() io.ReadCloser         { return r.stdout }
func (r *processRunner) Stderr() io.ReadCloser         { return r.stderr }
func (*processRunner) Name() string                    { return "verified ConfigDrive process" }
func (*processRunner) Diagnose(context.Context) string { return "external ConfigDrive process failed" }
func (r *processRunner) ID() string {
	if r.cmd.Process == nil {
		return ""
	}
	return strconv.Itoa(r.cmd.Process.Pid)
}
func (r *processRunner) PluginToHost(network, address string) (string, string, error) {
	if network == "tcp" {
		host, port, err := net.SplitHostPort(address)
		ip := net.ParseIP(host)
		number, portErr := strconv.Atoi(port)
		if err != nil || portErr != nil || number < 1 || number > 65535 || ip == nil || !ip.IsLoopback() {
			return "", "", errors.New("plugin endpoint must be loopback")
		}
		return network, address, nil
	}
	if network == "unix" && r.directory != "" {
		relative, err := filepath.Rel(r.directory, address)
		if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return network, address, nil
		}
	}
	return "", "", errors.New("unsupported external plugin endpoint")
}
func (r *processRunner) HostToPlugin(network, address string) (string, string, error) {
	return r.PluginToHost(network, address)
}
