package fsdriver

import (
	"os"
	"path/filepath"
	"strconv"
)

// basicDriver is a simple default implementation that sends calls out to the "os"
// package. Extend the  basicDriver" type in system-specific files to add support,
// such as xattrs, which can add support at compile time.
type basicDriver struct{}

var _ Driver = &basicDriver{}

func (d *basicDriver) Open(p string) (File, error) {
	return os.Open(p)
}

func (d *basicDriver) Stat(p string) (os.FileInfo, error) {
	return os.Stat(p)
}

func (d *basicDriver) Lstat(p string) (os.FileInfo, error) {
	return os.Lstat(p)
}

func (d *basicDriver) Readlink(p string) (string, error) {
	return os.Readlink(p)
}

func (d *basicDriver) Mkdir(p string, mode os.FileMode) error {
	return os.Mkdir(p, mode)
}

// Remove is used to unlink files and remove directories.
// This is following the golang os package api which
// combines the operations into a higher level Remove
// function. If explicit unlinking or directory removal
// to mirror system call is required, they should be
// split up at that time.
func (d *basicDriver) Remove(path string) error {
	return os.Remove(path)
}

func (d *basicDriver) Link(oldname, newname string) error {
	return os.Link(oldname, newname)
}

func (d *basicDriver) Lchown(name, uidStr, gidStr string) error {
	uid, err := strconv.Atoi(uidStr)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(gidStr)
	if err != nil {
		return err
	}
	return os.Lchown(name, uid, gid)
}

func (d *basicDriver) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

func (d *basicDriver) Join(pathName ...string) string {
	return filepath.Join(pathName...)
}

func (d *basicDriver) IsAbs(pathName string) bool {
	return filepath.IsAbs(pathName)
}

func (d *basicDriver) Rel(base, target string) (string, error) {
	return filepath.Rel(base, target)
}

func (d *basicDriver) Base(pathName string) string {
	return filepath.Base(pathName)
}

func (d *basicDriver) Dir(pathName string) string {
	return filepath.Dir(pathName)
}

func (d *basicDriver) Clean(pathName string) string {
	return filepath.Clean(pathName)
}

func (d *basicDriver) Split(pathName string) (dir, file string) {
	return filepath.Split(pathName)
}
