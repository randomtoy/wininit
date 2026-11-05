// Package diagnostics creates redacted support archives.
package diagnostics

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/randomtoy/wininit/internal/config"
	"gopkg.in/yaml.v3"
)

const maxLogBytes = 10 << 20

type Manifest struct {
	CreatedAt time.Time `json:"created_at"`
	Notice    string    `json:"notice"`
}

// Collect writes a ZIP containing an allowlisted configuration projection,
// supplied status, a manifest, and regular .log files only.
func Collect(ctx context.Context, cfg config.Config, status any, output string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(output)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("create diagnostics output directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".wininit-diagnostics-*.zip")
	if err != nil {
		return fmt.Errorf("create diagnostics archive: %w", err)
	}
	tempName := temp.Name()
	defer func() {
		_ = temp.Close()
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()
	zw := zip.NewWriter(temp)
	if err = addJSON(zw, "manifest.json", Manifest{CreatedAt: time.Now().UTC(), Notice: "No raw metadata, user-data, credentials, tokens, or private keys are collected."}); err != nil {
		return err
	}
	if err = addJSON(zw, "status.json", status); err != nil {
		return err
	}
	sanitized, marshalErr := yaml.Marshal(cfg.Sanitized())
	if marshalErr != nil {
		return fmt.Errorf("marshal sanitized configuration: %w", marshalErr)
	}
	if err = addBytes(zw, "config/wininit.sanitized.yaml", sanitized); err != nil {
		return err
	}
	if err = addLogs(ctx, zw, cfg.Log.Directory); err != nil {
		return err
	}
	if err = zw.Close(); err != nil {
		return fmt.Errorf("finalize diagnostics ZIP: %w", err)
	}
	if err = temp.Sync(); err != nil {
		return fmt.Errorf("sync diagnostics ZIP: %w", err)
	}
	if err = temp.Close(); err != nil {
		return fmt.Errorf("close diagnostics ZIP: %w", err)
	}
	if _, statErr := os.Stat(output); statErr == nil {
		return fmt.Errorf("diagnostics output already exists: %s", output)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("check diagnostics output: %w", statErr)
	}
	if err = os.Rename(tempName, output); err != nil {
		return fmt.Errorf("publish diagnostics ZIP: %w", err)
	}
	return nil
}

func addJSON(zw *zip.Writer, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	return addBytes(zw, name, append(data, '\n'))
}

func addBytes(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
	if err != nil {
		return fmt.Errorf("create ZIP entry %s: %w", name, err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write ZIP entry %s: %w", name, err)
	}
	return nil
}

func addLogs(ctx context.Context, zw *zip.Writer, directory string) error {
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read log directory: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		file, err := os.Open(filepath.Join(directory, entry.Name()))
		if err != nil {
			return fmt.Errorf("open log %s: %w", entry.Name(), err)
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "logs/" + entry.Name(), Method: zip.Deflate})
		if err == nil {
			_, err = io.CopyN(w, file, min(info.Size(), maxLogBytes))
		}
		closeErr := file.Close()
		if err != nil && err != io.EOF {
			return fmt.Errorf("archive log %s: %w", entry.Name(), err)
		}
		if closeErr != nil {
			return fmt.Errorf("close log %s: %w", entry.Name(), closeErr)
		}
	}
	return nil
}
