//go:build !windows

package hostname

import "context"

// Snapshot is unavailable outside Windows.
func (*Adapter) Snapshot(ctx context.Context) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	return "", "", ErrUnsupported
}

// Set is unavailable outside Windows.
func (*Adapter) Set(ctx context.Context, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return ErrUnsupported
}
