package fsdriver

import (
	"os"

	"github.com/containerd/continuity/common"
	"github.com/containerd/continuity/devices"
)

// lowDriver is the Linux on Windows lowDriver that allows manipulating
// files on a remote linux filesystem on Windows like a NFS client.
// Right now, pretend it's a local file system. Later on, this can be
// implemented through a NFS client, 9p client or another way.

type lowDriver struct{}

var _ Driver = &lowDriver{}
var _ XAttrDriver = &lowDriver{}
var _ LXAttrDriver = &lowDriver{}
var _ DeviceInfoDriver = &lowDriver{}

func (d *lowDriver) Open(p string) (File, error) {
	file, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	return &lowFile{file}, nil
}

func (d *lowDriver) Stat(p string) (os.FileInfo, error) {
	return os.Stat(p)
}

func (d *lowDriver) Lstat(p string) (os.FileInfo, error) {
	return os.Lstat(p)
}

func (d *lowDriver) Readlink(p string) (string, error) {
	return os.Readlink(p)
}

func (d *lowDriver) Mkdir(p string, mode os.FileMode) error {
	return os.Mkdir(p, mode)
}

func (d *lowDriver) Remove(path string) error {
	return os.Remove(path)
}

func (d *lowDriver) Link(oldname, newname string) error {
	return os.Link(oldname, newname)
}

func (d *lowDriver) Lchown(name string, uid, gid int64) error {
	return os.Lchown(name, int(uid), int(gid))
}

func (d *lowDriver) Symlink(oldname, newname string) error {
	return os.Symlink(oldname, newname)
}

func (d *lowDriver) Mknod(path string, mode os.FileMode, major, minor int) error {
	return common.ErrNotSupported
}

func (d *lowDriver) Mkfifo(path string, mode os.FileMode) error {
	return common.ErrNotSupported
}

// Lchmod changes the mode of an file not following symlinks.
func (d *lowDriver) Lchmod(path string, mode os.FileMode) (err error) {
	return common.ErrNotSupported
}

// Getxattr returns all of the extended attributes for the file at path p.
func (d *lowDriver) Getxattr(p string) (map[string][]byte, error) {
	return nil, common.ErrNotSupported
}

// Setxattr sets all of the extended attributes on file at path, following
// any symbolic links, if necessary. All attributes on the target are
// replaced by the values from attr. If the operation fails to set any
// attribute, those already applied will not be rolled back.
func (d *lowDriver) Setxattr(path string, attrMap map[string][]byte) error {
	return common.ErrNotSupported
}

// LGetxattr returns all of the extended attributes for the file at path p
// not following symbolic links.
func (d *lowDriver) LGetxattr(p string) (map[string][]byte, error) {
	return nil, common.ErrNotSupported
}

func (d *lowDriver) LSetxattr(path string, attrMap map[string][]byte) error {
	return common.ErrNotSupported
}

func (d *lowDriver) DeviceInfo(fi os.FileInfo) (maj uint64, min uint64, err error) {
	return devices.DeviceInfo(fi)
}
