package continuity

import (
	"fmt"
	"os"
)

var (
	errdeviceInfoNotImplemented = fmt.Errorf("deviceInfo not implemented on Windows")
	errLchmodNotImplemented     = fmt.Errorf("Lchmod not implemented on Windows")
	errMknodNotImplemented      = fmt.Errorf("Mknod not implemented on Windows")
	errMkfifoNotImplemented     = fmt.Errorf("Mkfifo not implemented on Windows")
)

func deviceInfo(fi os.FileInfo) (maj uint64, min uint64, err error) {
	return 0, 0, errdeviceInfoNotImplemented
}

// Lchmod changes the mode of an file not following symlinks.
func (d *driver) Lchmod(path string, mode os.FileMode) (err error) {
	return errLchmodNotImplemented
}

func (d *driver) Mknod(path string, mode os.FileMode, major, minor int) error {
	return errMknodNotImplemented
}

func (d *driver) Mkfifo(path string, mode os.FileMode) error {
	return errMkfifoNotImplemented
}
