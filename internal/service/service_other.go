//go:build !windows

package service

import (
	"context"
	"errors"
	"log/slog"
)

var errUnsupported = errors.New("Windows service management is unavailable on this platform")

type platformManager struct{}

func newPlatformManager() Manager                               { return &platformManager{} }
func isWindowsService() (bool, error)                           { return false, nil }
func runWindowsService(*slog.Logger, Runner) error              { return errUnsupported }
func (*platformManager) Install(context.Context, string) error  { return errUnsupported }
func (*platformManager) Uninstall(context.Context) error        { return errUnsupported }
func (*platformManager) Start(context.Context) error            { return errUnsupported }
func (*platformManager) Stop(context.Context) error             { return errUnsupported }
func (*platformManager) Status(context.Context) (string, error) { return "unavailable", errUnsupported }
