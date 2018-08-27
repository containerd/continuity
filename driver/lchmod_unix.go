// +build darwin freebsd solaris

package driver

import (
	"os"

	"golang.org/x/sys/unix"
)

// Lchmod changes the mode of a file not following symlinks.
func (d *driver) Lchmod(path string, mode os.FileMode) error {
	err := unix.Fchmodat(unix.AT_FDCWD, path, uint32(mode), unix.AT_SYMLINK_NOFOLLOW)
	if err != nil {
		err = &os.PathError{Op: "lchmod", Path: path, Err: err}
	}
	return err
}
