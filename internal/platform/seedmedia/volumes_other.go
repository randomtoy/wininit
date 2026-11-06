//go:build !windows

package seedmedia

import (
	"context"
	"errors"
)

func FindVolumes(ctx context.Context, _ string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("local seed volume discovery requires Windows; configure a local seed_directory for development")
}
