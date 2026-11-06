// Package identity implements local Windows identity operations without a shell.
package identity

type Adapter struct{}

func New() *Adapter { return &Adapter{} }
