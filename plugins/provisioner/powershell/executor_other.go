//go:build !windows

package powershell

import (
	"context"
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
)

type NativeExecutor struct{}

func (NativeExecutor) Run(context.Context, []byte, uint32) (*userdatav1.Execution, error) {
	return nil, userdatav1.ErrUnavailableRuntime
}
