// Package logging creates structured process logs.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/randomtoy/wininit/internal/config"
)

func New(cfg config.LogConfig, console io.Writer) (*slog.Logger, io.Closer, error) {
	if err := os.MkdirAll(cfg.Directory, 0o750); err != nil {
		return nil, nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(cfg.Directory, "wininit.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, fmt.Errorf("open log file: %w", err)
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(cfg.Level)); err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("parse log level: %w", err)
	}
	writer := io.Writer(file)
	if console != nil {
		writer = io.MultiWriter(file, console)
	}
	logger := slog.New(slog.NewJSONHandler(writer, &slog.HandlerOptions{Level: level}))
	return logger, file, nil
}
