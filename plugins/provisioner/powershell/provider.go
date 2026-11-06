// Package powershell implements the separately built PowerShell process plugin.
// The agent host must not import this package or receive its script/output bytes.
package powershell

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
	"unicode/utf8"

	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
)

type Executor interface {
	Run(context.Context, []byte, uint32) (*userdatav1.Execution, error)
}
type Provider struct {
	mu       sync.Mutex
	prepared bool
	executed bool
	lease    string
	script   []byte
	executor Executor
}

func New(executor Executor) *Provider { return &Provider{executor: executor} }
func (p *Provider) Prepare(ctx context.Context, request *userdatav1.PrepareRequest) (*userdatav1.Prepared, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prepared || p.executed {
		return nil, userdatav1.ErrInvalidData
	}
	digest, err := hex.DecodeString(request.GetApprovedSha256())
	if err != nil || len(digest) != sha256.Size {
		return nil, userdatav1.ErrApproval
	}
	data, err := readUserData(ctx, request)
	if errors.Is(err, errMissing) {
		return &userdatav1.Prepared{Unavailable: true}, nil
	}
	if err != nil {
		return nil, err
	}
	defer clear(data)
	sum := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(sum[:], digest) != 1 {
		return nil, userdatav1.ErrApproval
	}
	script, err := parseScript(data)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	p.script = bytes.Clone(script)
	p.lease = hex.EncodeToString(random)
	p.prepared = true
	return &userdatav1.Prepared{LeaseId: p.lease, Sha256: hex.EncodeToString(sum[:]), Size: uint32(len(data))}, nil
}
func (p *Provider) Execute(ctx context.Context, request *userdatav1.ExecuteRequest) (*userdatav1.Execution, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.prepared || p.executed || len(request.GetLeaseId()) != len(p.lease) || subtle.ConstantTimeCompare([]byte(request.GetLeaseId()), []byte(p.lease)) != 1 {
		return nil, userdatav1.ErrInvalidData
	}
	if request.GetMaxOutputBytes() < 1024 || request.GetMaxOutputBytes() > userdatav1.MaxOutputBytes {
		return nil, userdatav1.ErrInvalidData
	}
	p.executed = true
	defer func() { clear(p.script); p.script = nil; p.lease = "" }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p.executor == nil {
		return nil, userdatav1.ErrUnavailableRuntime
	}
	return p.executor.Run(ctx, p.script, request.GetMaxOutputBytes())
}
func parseScript(data []byte) ([]byte, error) {
	if len(data) > userdatav1.MaxScriptBytes || !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, userdatav1.ErrInvalidData
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	header, body, ok := bytes.Cut(data, []byte("\n"))
	header = bytes.TrimSuffix(header, []byte("\r"))
	if !ok || (!bytes.Equal(header, []byte("#ps1_sysnative")) && !bytes.Equal(header, []byte("#ps1"))) || len(bytes.TrimSpace(body)) == 0 {
		return nil, userdatav1.ErrInvalidData
	}
	return body, nil
}
