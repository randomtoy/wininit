//go:build !windows

package state

import "context"

func AcquireMachineLock(context.Context) (func(), error) {
	return nil, ErrUnsupported
}
