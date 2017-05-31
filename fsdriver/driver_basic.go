package fsdriver

import (
	"os"
	"path/filepath"
)

// basicDriver is a simple default implementation that sends calls out to the "os"
// package. Extend the  basicDriver" type in system-specific files to add support,
// such as xattrs, which can add support at compile time.
type basicDriver struct{}

var _ Driver = &basicDriver{}

func (*basicDriver) Open(p string) (File, error) {
	return os.Open(p)
}

func (*basicDriver) Stat(p string) (os.FileInfo, error) {
	return os.Stat(p)
}

func (*basicDriver) Lstat(p string) (os.FileInfo, error) {
	return os.Lstat(p)
}

func (*basicDriver) Readlink(p string) (string, error) {
	return os.Readlink(p)
}

func (*basicDriver) Mkdir(p string, mode os.FileMode) error {
	return os.Mkdir(p, mode)
}

// Remove is used to unlink files and remove directories.
// This is following the golang os package api which
// combines the operations into a higher level Remove
// function. If explicit unlinking or directory removal
// to mirror system call is required, they should be
// split up at that time.
func (*basicDriver) Remove(path string) error {
	return os.Remove(path)
}

func (*basicDriver) Link(oldname, newname string) error {
	return os.Link(oldname, newname)
}

func (*basicDriver) Lchown(name string, uid, gid int64) error {
	// TODO: error out if uid excesses int bit width?
	return os.Lchown(name, int(uid), int(gid))
}

func (*basicDriver) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

func (*basicDriver) Join(pathName ...string) string {
	return filepath.Join(pathName...)
}

func (*basicDriver) IsAbs(pathName string) bool {
	return filepath.IsAbs(pathName)
}

func (*basicDriver) Rel(base, target string) (string, error) {
	return filepath.Rel(base, target)
}

func (*basicDriver) Base(pathName string) string {
	return filepath.Base(pathName)
}

func (*basicDriver) Dir(pathName string) string {
	return filepath.Dir(pathName)
}

func (*basicDriver) Clean(pathName string) string {
	return filepath.Clean(pathName)
}

func (*basicDriver) Split(pathName string) (dir, file string) {
	return filepath.Split(pathName)
}

func (*basicDriver) Separator() byte {
	return filepath.Separator
}

func (*basicDriver) NormalizePath(pathName string) string {
	// Windows accepts '/' as a path separator, so turn those to '\'
	// Noops on other platforms.
	return filepath.FromSlash(pathName)
}
