//go:build windows

package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/registry"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type platformManager struct{}

func newPlatformManager() Manager { return &platformManager{} }

func isWindowsService() (bool, error) { return svc.IsWindowsService() }

func runWindowsService(logger *slog.Logger, runner Runner) error {
	return svc.Run(Name, &handler{logger: logger, runner: runner})
}

type handler struct {
	logger *slog.Logger
	runner Runner
}

func (h *handler) Execute(_ []string, changes <-chan svc.ChangeRequest, statuses chan<- svc.Status) (bool, uint32) {
	statuses <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.runner(ctx) }()
	current := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	statuses <- current
	for {
		select {
		case err := <-done:
			statuses <- svc.Status{State: svc.StopPending}
			if err != nil {
				h.logger.Error("service application failed", "error", err)
				return false, 1
			}
			return false, 0
		case request := <-changes:
			switch request.Cmd {
			case svc.Interrogate:
				statuses <- current
			case svc.Stop, svc.Shutdown:
				statuses <- svc.Status{State: svc.StopPending}
				cancel()
				if err := <-done; err != nil {
					h.logger.Error("service shutdown failed", "error", err)
					return false, 1
				}
				return false, 0
			}
		}
	}
}

func (*platformManager) Install(ctx context.Context, executable string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if managedByMSI() {
		return errors.New("installation is MSI-managed; use the MSI for service registration changes")
	}
	absolute, err := filepath.Abs(executable)
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager: %w", err)
	}
	defer manager.Disconnect()
	service, err := manager.CreateService(Name, absolute, mgr.Config{
		DisplayName: DisplayName,
		Description: "Windows guest initialization agent foundation",
		StartType:   mgr.StartAutomatic,
	})
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	return service.Close()
}

func (*platformManager) Uninstall(ctx context.Context) error {
	if managedByMSI() {
		return errors.New("installation is MSI-managed; uninstall or repair the MSI instead")
	}
	manager, service, err := openService()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	defer service.Close()
	_ = stopAndWait(ctx, service)
	if err := service.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func (*platformManager) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	manager, service, err := openService()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	defer service.Close()
	if err := service.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return waitForState(ctx, service, svc.Running)
}

func (*platformManager) Stop(ctx context.Context) error {
	manager, service, err := openService()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	defer service.Close()
	return stopAndWait(ctx, service)
}

func (*platformManager) Status(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	manager, service, err := openService()
	if err != nil {
		return "not_installed", err
	}
	defer manager.Disconnect()
	defer service.Close()
	status, err := service.Query()
	if err != nil {
		return "unknown", fmt.Errorf("query service: %w", err)
	}
	return stateName(status.State), nil
}

func openService() (*mgr.Mgr, *mgr.Service, error) {
	manager, err := mgr.Connect()
	if err != nil {
		return nil, nil, fmt.Errorf("connect to service manager: %w", err)
	}
	service, err := manager.OpenService(Name)
	if err != nil {
		manager.Disconnect()
		return nil, nil, fmt.Errorf("open service: %w", err)
	}
	return manager, service, nil
}

func stopAndWait(ctx context.Context, service *mgr.Service) error {
	status, err := service.Query()
	if err != nil {
		return fmt.Errorf("query service: %w", err)
	}
	if status.State == svc.Stopped {
		return nil
	}
	if _, err := service.Control(svc.Stop); err != nil {
		return fmt.Errorf("request service stop: %w", err)
	}
	return waitForState(ctx, service, svc.Stopped)
}

type statusQuerier interface {
	Query() (svc.Status, error)
}

func waitForState(ctx context.Context, service statusQuerier, wanted svc.State) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := service.Query()
		if err != nil {
			return fmt.Errorf("query service: %w", err)
		}
		if status.State == wanted {
			return nil
		}
		if wanted == svc.Running && status.State == svc.Stopped {
			return fmt.Errorf("service stopped before reaching running state (Windows exit code %d, service exit code %d)", status.Win32ExitCode, status.ServiceSpecificExitCode)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func managedByMSI() bool {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Wininit\Installation`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer key.Close()
	value, _, err := key.GetStringValue("ManagedBy")
	return err == nil && value == "MSI"
}

func stateName(value svc.State) string {
	switch value {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start_pending"
	case svc.StopPending:
		return "stop_pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue_pending"
	case svc.PausePending:
		return "pause_pending"
	case svc.Paused:
		return "paused"
	default:
		return "unknown"
	}
}

func currentExecutable() (string, error) { return os.Executable() }
