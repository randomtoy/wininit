// Package app wires CLI and Windows service entry points to the core.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/randomtoy/wininit/internal/config"
	"github.com/randomtoy/wininit/internal/diagnostics"
	"github.com/randomtoy/wininit/internal/lifecycle"
	"github.com/randomtoy/wininit/internal/logging"
	"github.com/randomtoy/wininit/internal/plugin"
	"github.com/randomtoy/wininit/internal/service"
	"github.com/randomtoy/wininit/internal/state"
	"github.com/randomtoy/wininit/internal/version"
)

const usage = `wininit - Windows initialization agent

Usage:
  wininit version [--json]
  wininit run [--config PATH]
  wininit status [--config PATH] [--json]
  wininit validate [--config PATH]
  wininit collect-logs [--config PATH] [--output PATH]
  wininit service install|uninstall|start|stop|status

NoCloud, ConfigDrive and hostname plugins require explicit enablement in configuration.
MSI-managed installations must use MSI repair/uninstall for registration changes.
`

type Status struct {
	Service      string              `json:"service"`
	Lifecycle    *state.State        `json:"lifecycle,omitempty"`
	Provisioning string              `json:"provisioning"`
	Build        version.Info        `json:"build"`
	Plugins      []plugin.Descriptor `json:"plugins"`
	StateError   string              `json:"state_error,omitempty"`
	ServiceError string              `json:"service_error,omitempty"`
}

// Execute runs a CLI command and returns a process exit code.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, usage)
		return 0
	}
	var err error
	switch args[0] {
	case "version":
		err = commandVersion(args[1:], stdout, stderr)
	case "validate":
		err = commandValidate(args[1:], stdout, stderr)
	case "run":
		err = commandRun(ctx, args[1:], stderr)
	case "status":
		err = commandStatus(ctx, args[1:], stdout, stderr)
	case "collect-logs":
		err = commandCollect(ctx, args[1:], stdout, stderr)
	case "service":
		err = commandService(ctx, args[1:], stdout)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	return 0
}

// RunService loads the installer configuration and enters SCM dispatch.
func RunService() error {
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		return err
	}
	logger, closer, err := logging.New(cfg.Log, nil)
	if err != nil {
		return err
	}
	defer closer.Close()
	store := state.NewRegistryStore(cfg.State.RegistryPath)
	engine := lifecycle.NewWithPlugins(store, logger, configuredPlugins(cfg), pluginConfigurationHash(cfg))
	return service.Run(logger, func(ctx context.Context) error {
		logger.InfoContext(ctx, "acquiring lifecycle lock")
		release, err := state.AcquireMachineLock(ctx)
		if err != nil {
			return err
		}
		defer release()
		logger.InfoContext(ctx, "lifecycle lock acquired")
		return engine.Run(ctx)
	})
}

func commandVersion(args []string, stdout, stderr io.Writer) error {
	set := newFlagSet("version", stderr)
	jsonOutput := set.Bool("json", false, "write JSON")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return errors.New("version accepts no positional arguments")
	}
	info := version.Current()
	if *jsonOutput {
		return writeJSON(stdout, info)
	}
	fmt.Fprintf(stdout, "wininit %s (commit %s, built %s, %s/%s, %s)\n", info.Version, info.Commit, info.BuildDate, info.OS, info.Arch, info.GoVersion)
	return nil
}

func commandValidate(args []string, stdout, stderr io.Writer) error {
	set := newFlagSet("validate", stderr)
	path := set.String("config", config.DefaultPath(), "configuration file")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return errors.New("validate accepts no positional arguments")
	}
	if _, err := config.Load(*path); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "configuration %s is valid (schema version %d)\n", *path, config.CurrentVersion)
	return nil
}

func commandRun(ctx context.Context, args []string, stderr io.Writer) error {
	set := newFlagSet("run", stderr)
	path := set.String("config", config.DefaultPath(), "configuration file")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return errors.New("run accepts no positional arguments")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	logger, closer, err := logging.New(cfg.Log, stderr)
	if err != nil {
		return err
	}
	defer closer.Close()
	release, err := state.AcquireMachineLock(ctx)
	if err != nil {
		return err
	}
	defer release()
	return lifecycle.NewWithPlugins(state.NewRegistryStore(cfg.State.RegistryPath), logger, configuredPlugins(cfg), pluginConfigurationHash(cfg)).Run(ctx)
}

func commandStatus(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	set := newFlagSet("status", stderr)
	path := set.String("config", config.DefaultPath(), "configuration file")
	jsonOutput := set.Bool("json", false, "write JSON")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return errors.New("status accepts no positional arguments")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	report := collectStatus(ctx, cfg)
	if *jsonOutput {
		if err := writeJSON(stdout, report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "Service: %s\nProvisioning: %s\n", report.Service, report.Provisioning)
		for _, descriptor := range report.Plugins {
			fmt.Fprintf(stdout, "Plugin %s (%s): enabled=%t, status=%s\n", descriptor.Name, descriptor.Kind, descriptor.Enabled, descriptor.Status)
		}
		if report.Lifecycle == nil {
			fmt.Fprintln(stdout, "Lifecycle: uninitialized")
		} else {
			fmt.Fprintf(stdout, "Lifecycle: %s (schema %d, generation %d)\n", report.Lifecycle.Phase, report.Lifecycle.SchemaVersion, report.Lifecycle.Generation)
		}
		if report.ServiceError != "" {
			fmt.Fprintf(stdout, "Service query: %s\n", report.ServiceError)
		}
		if report.StateError != "" {
			fmt.Fprintf(stdout, "State query: %s\n", report.StateError)
		}
	}
	return nil
}

func commandCollect(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	set := newFlagSet("collect-logs", stderr)
	path := set.String("config", config.DefaultPath(), "configuration file")
	output := set.String("output", "", "diagnostic ZIP output")
	if err := set.Parse(args); err != nil {
		return err
	}
	if set.NArg() != 0 {
		return errors.New("collect-logs accepts no positional arguments")
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	if *output == "" {
		*output = fmt.Sprintf("wininit-diagnostics-%s.zip", time.Now().UTC().Format("20060102T150405Z"))
	}
	absolute, err := filepath.Abs(*output)
	if err != nil {
		return fmt.Errorf("resolve diagnostic output: %w", err)
	}
	if err := diagnostics.Collect(ctx, cfg, collectStatus(ctx, cfg), absolute); err != nil {
		return err
	}
	fmt.Fprintln(stdout, absolute)
	return nil
}

func commandService(ctx context.Context, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("service requires exactly one of: install, uninstall, start, stop, status")
	}
	manager := service.NewManager()
	var err error
	switch args[0] {
	case "install":
		if _, configErr := config.Load(config.DefaultPath()); configErr != nil {
			return fmt.Errorf("validate default service configuration before installation: %w", configErr)
		}
		executable, exeErr := os.Executable()
		if exeErr != nil {
			return fmt.Errorf("find executable: %w", exeErr)
		}
		err = manager.Install(ctx, executable)
	case "uninstall":
		err = manager.Uninstall(ctx)
	case "start":
		err = manager.Start(ctx)
	case "stop":
		err = manager.Stop(ctx)
	case "status":
		var status string
		status, err = manager.Status(ctx)
		if err == nil {
			fmt.Fprintln(stdout, status)
		}
	default:
		return fmt.Errorf("unknown service action %q", args[0])
	}
	if err != nil {
		return fmt.Errorf("service %s: %w", args[0], err)
	}
	if args[0] != "status" {
		fmt.Fprintf(stdout, "service %s completed\n", args[0])
	}
	return nil
}

func collectStatus(ctx context.Context, cfg config.Config) Status {
	report := Status{Provisioning: configuredProvisioning(cfg), Build: version.Current(), Plugins: configuredPlugins(cfg).Descriptors()}
	serviceState, serviceErr := service.NewManager().Status(ctx)
	report.Service = serviceState
	if serviceErr != nil {
		report.ServiceError = serviceErr.Error()
	}
	lifecycleState, stateErr := state.NewRegistryStore(cfg.State.RegistryPath).Load(ctx)
	if stateErr == nil {
		report.Lifecycle = &lifecycleState
		applySavedPluginStatus(&report, lifecycleState, pluginConfigurationHash(cfg))
	} else if !errors.Is(stateErr, state.ErrNotFound) {
		report.StateError = stateErr.Error()
	}
	return report
}

func applySavedPluginStatus(report *Status, current state.State, configurationHash string) {
	if report.Provisioning == "disabled" || current.ConfigurationHash != configurationHash || current.Provisioning == "" {
		return
	}
	report.Provisioning = current.Provisioning
	for index := range report.Plugins {
		if report.Plugins[index].Enabled {
			if checkpoint, ok := current.Plugins[report.Plugins[index].Name]; ok {
				report.Plugins[index].Status = checkpoint.Status
			}
		}
	}
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(stderr)
	return set
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// SignalContext returns a process context for foreground mode.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func IsHelp(args []string) bool {
	return len(args) == 0 || strings.EqualFold(args[0], "help")
}
