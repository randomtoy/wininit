package powershell

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"sync/atomic"
	"unicode/utf16"
)

// Script text is sent only through an owned stdin pipe. The command line is a
// constant bootstrap, never an interpolated/encoded copy of user-data.
const bootstrap = `$ErrorActionPreference='Stop'; $ProgressPreference='SilentlyContinue'; [Console]::InputEncoding=New-Object System.Text.UTF8Encoding($false,$true); try { $source=[Console]::In.ReadToEnd(); $global:LASTEXITCODE=0; . ([ScriptBlock]::Create($source)); exit $LASTEXITCODE } catch { exit 1 }`

func bootstrapArguments() []string {
	words := utf16.Encode([]rune(bootstrap))
	encoded := make([]byte, len(words)*2)
	for i, word := range words {
		binary.LittleEndian.PutUint16(encoded[i*2:], word)
	}
	return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded)}
}

var errOutputLimit = errors.New("PowerShell output limit exceeded")

type outputCounter struct {
	count    atomic.Uint64
	limit    uint64
	cancel   context.CancelFunc
	exceeded atomic.Bool
}

func (w *outputCounter) Write(data []byte) (int, error) {
	if w.count.Add(uint64(len(data))) > w.limit {
		w.exceeded.Store(true)
		w.cancel()
		return 0, errOutputLimit
	}
	return len(data), nil
}
func (w *outputCounter) Bytes() uint64 {
	value := w.count.Load()
	if value > w.limit {
		return w.limit + 1
	}
	return value
}
