//go:build windows

package powershell

import (
	"errors"
	"github.com/randomtoy/wininit/internal/platform/securefile"
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
	"golang.org/x/sys/windows"
	"io"
	"os"
)

type windowsDirectory struct{ file *os.File }

func openDirectory(path string) (directory, error) {
	file, err := securefile.OpenLocal(path, windows.FILE_LIST_DIRECTORY|windows.FILE_TRAVERSE, windows.FILE_DIRECTORY_FILE)
	if err != nil {
		return nil, userdatav1.ErrSource
	}
	return &windowsDirectory{file}, nil
}
func (d *windowsDirectory) Close() error { return d.file.Close() }
func (d *windowsDirectory) Subdir(name string) (directory, error) {
	file, err := securefile.OpenRelative(windows.Handle(d.file.Fd()), name, windows.FILE_LIST_DIRECTORY|windows.FILE_TRAVERSE, windows.FILE_OPEN, windows.FILE_DIRECTORY_FILE, nil)
	if errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) || errors.Is(err, windows.STATUS_OBJECT_PATH_NOT_FOUND) {
		return nil, errMissing
	}
	if err != nil {
		return nil, userdatav1.ErrInvalidData
	}
	return &windowsDirectory{file}, nil
}
func (d *windowsDirectory) Read(name string, limit int) ([]byte, error) {
	file, err := securefile.OpenRelative(windows.Handle(d.file.Fd()), name, windows.GENERIC_READ, windows.FILE_OPEN, windows.FILE_NON_DIRECTORY_FILE, nil)
	if errors.Is(err, windows.STATUS_OBJECT_NAME_NOT_FOUND) {
		return nil, errMissing
	}
	if err != nil {
		return nil, userdatav1.ErrInvalidData
	}
	defer file.Close()
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(windows.Handle(file.Fd()), &info); err != nil || info.NumberOfLinks != 1 || info.FileSizeHigh != 0 || info.FileSizeLow > uint32(limit) {
		return nil, userdatav1.ErrInvalidData
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) > limit {
		clear(data)
		return nil, userdatav1.ErrInvalidData
	}
	return data, nil
}
