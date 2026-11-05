// Package hostname provides operating-system computer-name operations.
package hostname

import "errors"

// ErrUnsupported indicates that hostname mutation is unavailable on this OS.
var ErrUnsupported = errors.New("hostname provisioning is supported only on Windows")

// Adapter implements the hostname provisioner's platform contract.
type Adapter struct{}

// New constructs a hostname platform adapter.
func New() *Adapter {
	return &Adapter{}
}
