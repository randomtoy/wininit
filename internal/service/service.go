// Package service adapts the application to the Windows Service Control Manager.
package service

import (
	"context"
	"log/slog"
)

const (
	Name        = "wininit"
	DisplayName = "Wininit initialization agent"
)

type Runner func(context.Context) error

type Manager interface {
	Install(context.Context, string) error
	Uninstall(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Status(context.Context) (string, error)
}

func NewManager() Manager { return newPlatformManager() }

func IsWindowsService() (bool, error) { return isWindowsService() }

func Run(logger *slog.Logger, runner Runner) error { return runWindowsService(logger, runner) }
