//go:build !windows

package nocloud

import (
	"context"
	"errors"
)

func FindVolumes(context.Context) ([]string, error) {
	return nil, errors.New("NoCloud volume discovery requires Windows; configure a local seed_directory for development")
}
