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

type processRole uint8

const (
	roleDatasource processRole = iota
	rolePowerShell
	roleProvisioner
)

type processRunner struct {
	cmd            *exec.Cmd
	stdout, stderr io.ReadCloser
	control        *processControl
	directory      string
	cancelled      atomic.Bool
	once           sync.Once
	startFailure   error
	startCancelled bool
	exitStatus     atomic.Uint64
	finished       chan struct{}
	pid            int
	label          string
}

func newProcessRunner(ctx context.Context, cmd *exec.Cmd, directory string) (*processRunner, error) {
	return newProcessRunnerProfile(ctx, cmd, directory, roleDatasource, "ConfigDrive")
}
func newProcessRunnerProfile(ctx context.Context, cmd *exec.Cmd, directory string, role processRole, label string) (*processRunner, error) {
	control, err := prepareProcessProfile(ctx, cmd, role)
	if err != nil {
		return nil, err
	}
	r := &processRunner{cmd: cmd, control: control, directory: directory, finished: make(chan struct{}), label: label}
	r.stdout, err = cmd.StdoutPipe()
	if err != nil {
		control.close()
		return nil, err
	}
	r.stderr, err = cmd.StderrPipe()
	if err != nil {
		r.stdout.Close()
		if file, ok := cmd.Stdout.(*os.File); ok {
			file.Close()
		}
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
	if err := r.control.start(r.cmd); err != nil {
		r.startCancelled = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		r.startFailure = processErrorFor(r.label, "process creation", err)
		r.close()
		return err
	}
	r.pid = r.cmd.Process.Pid
	if err := r.control.started(r.cmd); err != nil {
		r.startCancelled = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		r.startFailure = processErrorFor(r.label, "process containment", err)
		r.control.kill(r.cmd)
		r.control.wait(r.cmd)
		r.close()
		return err
	}
	return nil
}

// Expose only host operation names and numeric OS codes, never child payloads.
func processError(operation string, cause error) error {
	return processErrorFor("ConfigDrive", operation, cause)
}
func processErrorFor(label, operation string, cause error) error {
	var code syscall.Errno
	if errors.As(cause, &code) {
		return fmt.Errorf("external %s %s failed (OS code %d)", label, operation, uint32(code))
	}
	return fmt.Errorf("external %s %s failed", label, operation)
}
func (r *processRunner) close() {
	r.once.Do(func() {
		r.control.close()
		r.stdout.Close()
		r.stderr.Close()
		if file, ok := r.cmd.Stdout.(*os.File); ok {
			file.Close()
		}
		if file, ok := r.cmd.Stderr.(*os.File); ok {
			file.Close()
		}
	})
}
func (r *processRunner) Wait(context.Context) error {
	defer func() { r.close(); close(r.finished) }()
	code, err := r.control.wait(r.cmd)
	r.exitStatus.Store(1<<32 | uint64(code))
	return err
}
func (r *processRunner) Kill(context.Context) error {
	err := r.control.kill(r.cmd)
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	return err
}
func (r *processRunner) Stdout() io.ReadCloser { return r.stdout }
func (r *processRunner) Stderr() io.ReadCloser { return r.stderr }
func (r *processRunner) Name() string          { return "verified " + r.label + " process" }
func (r *processRunner) Diagnose(context.Context) string {
	return "external " + r.label + " process failed"
}
func (r *processRunner) ID() string {
	if r.pid == 0 {
		return ""
	}
	return strconv.Itoa(r.pid)
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
