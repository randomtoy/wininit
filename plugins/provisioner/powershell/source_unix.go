//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package powershell

import (
	userdatav1 "github.com/randomtoy/wininit/sdk/userdata/v1"
	"io"
	"os"
	"syscall"
)

type portableDirectory struct{ root *os.Root }

func openDirectory(path string) (directory, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return nil, userdatav1.ErrSource
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, userdatav1.ErrSource
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, userdatav1.ErrSource
	}
	return &portableDirectory{root}, nil
}
func (d *portableDirectory) Close() error { return d.root.Close() }
func (d *portableDirectory) Subdir(name string) (directory, error) {
	info, err := d.root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, errMissing
	}
	if err != nil || !info.IsDir() {
		return nil, userdatav1.ErrInvalidData
	}
	root, err := d.root.OpenRoot(name)
	if err != nil {
		return nil, userdatav1.ErrInvalidData
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, userdatav1.ErrInvalidData
	}
	return &portableDirectory{root}, nil
}
func (d *portableDirectory) Read(name string, limit int) ([]byte, error) {
	info, err := d.root.Lstat(name)
	if os.IsNotExist(err) {
		return nil, errMissing
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, userdatav1.ErrInvalidData
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, userdatav1.ErrInvalidData
	}
	file, err := d.root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, userdatav1.ErrInvalidData
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, userdatav1.ErrInvalidData
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil || len(data) > limit {
		clear(data)
		return nil, userdatav1.ErrInvalidData
	}
	return data, nil
}
