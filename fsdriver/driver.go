package fsdriver

import (
	"os"
	"runtime"

	"github.com/containerd/continuity/common"
)

type DriverType int

const (
	// Basic is essentially a wrapper around the golang os package.
	// LOW is the Linux on Windows driver that lets the user manipulate remote
	// Linux filesystem files on Windows.
	Basic DriverType = iota
	LOW
)

// BasicDriver is exported as a global since it's just a wrapper around
// the os + filepath functions, so it has no internal state.
var BasicDriver Driver = &basicDriver{}

// Driver provides all of the system-level functions in a common interface.
// The context should call these with full paths and should never use the `os`
// package or any other package to access resources on the filesystem. This
// mechanism let's us carefully control access to the context and maintain
// path and resource integrity. It also gives us an interface to reason about
// direct resource access.
//
// Implementations don't need to do much other than meet the interface. For
// example, it is not required to wrap os.FileInfo to return correct paths for
// the call to Name().
type Driver interface {
	Open(path string) (File, error)
	Stat(path string) (os.FileInfo, error)
	Lstat(path string) (os.FileInfo, error)
	Readlink(p string) (string, error)
	Mkdir(path string, mode os.FileMode) error
	Remove(path string) error

	Link(oldname, newname string) error
	Lchmod(path string, mode os.FileMode) error
	Lchown(path string, uid, gid int64) error
	Symlink(oldname, newname string) error

	// TODO(aaronl): These methods might move outside the main Driver
	// interface in the future as more platforms are added.
	Mknod(path string, mode os.FileMode, major int, minor int) error
	Mkfifo(path string, mode os.FileMode) error

	// NOTE(stevvooe): We may want to actually include the path manipulation
	// functions here, as well. They have been listed below to make the
	// discovery process easier.
	Join(pathName ...string) string
	IsAbs(pathName string) bool
	Rel(base, target string) (string, error)
	Base(pathName string) string
	Dir(pathName string) string
	Clean(pathName string) string
	Split(pathName string) (dir, file string)
	// Abs(pathName string) (string, error)
	// Walk(string, filepath.WalkFunc) error
}

// Unfortunately, os.File is a struct instead of an interface, an interface
// has to be manually defined.
var _ File = &os.File{}

type File interface {
	Chdir() error
	Chmod(mode os.FileMode) error
	Chown(uid, gid int) error
	Close() error
	Fd() uintptr
	Name() string
	Read(b []byte) (n int, err error)
	ReadAt(b []byte, off int64) (n int, err error)
	Readdir(n int) ([]os.FileInfo, error)
	Readdirnames(n int) (names []string, err error)
	Seek(offset int64, whence int) (ret int64, err error)
	Stat() (os.FileInfo, error)
	Sync() error
	Truncate(size int64) error
	Write(b []byte) (n int, err error)
	WriteAt(b []byte, off int64) (n int, err error)
	WriteString(s string) (n int, err error)
}

func NewSystemDriver(driverType DriverType) (Driver, error) {
	// TODO(stevvooe): Consider having this take a "hint" path argument, which
	// would be the context root. The hint could be used to resolve required
	// filesystem support when assembling the driver to use.
	switch driverType {
	case Basic:
		return BasicDriver, nil
	case LOW:
		if runtime.GOOS != "windows" {
			return nil, common.ErrNotSupported
		}
		return &lowDriver{}, nil
	default:
		return nil, common.ErrNotSupported
	}
}

// XAttrDriver should be implemented on operation systems and filesystems that
// have xattr support for regular files and directories.
type XAttrDriver interface {
	// Getxattr returns all of the extended attributes for the file at path.
	// Typically, this takes a syscall call to Listxattr and Getxattr.
	Getxattr(path string) (map[string][]byte, error)

	// Setxattr sets all of the extended attributes on file at path, following
	// any symbolic links, if necessary. All attributes on the target are
	// replaced by the values from attr. If the operation fails to set any
	// attribute, those already applied will not be rolled back.
	Setxattr(path string, attr map[string][]byte) error
}

// LXAttrDriver should be implemented by drivers on operating systems and
// filesystems that support setting and getting extended attributes on
// symbolic links. If this is not implemented, extended attributes will be
// ignored on symbolic links.
type LXAttrDriver interface {
	// LGetxattr returns all of the extended attributes for the file at path
	// and does not follow symlinks. Typically, this takes a syscall call to
	// Llistxattr and Lgetxattr.
	LGetxattr(path string) (map[string][]byte, error)

	// LSetxattr sets all of the extended attributes on file at path, without
	// following symbolic links. All attributes on the target are replaced by
	// the values from attr. If the operation fails to set any attribute,
	// those already applied will not be rolled back.
	LSetxattr(path string, attr map[string][]byte) error
}

type DeviceInfoDriver interface {
	DeviceInfo(fi os.FileInfo) (maj uint64, min uint64, err error)
}
