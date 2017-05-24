package fsdriver

import (
	"os"

	"github.com/containerd/continuity/common"
	"github.com/pkg/errors"
)

func (*basicDriver) Mknod(path string, mode os.FileMode, major, minor int) error {
	return errors.Wrap(common.ErrNotSupported, "cannot create device node on Windows")
}

func (*basicDriver) Mkfifo(path string, mode os.FileMode) error {
	return errors.Wrap(common.ErrNotSupported, "cannot create fifo on Windows")
}

// Lchmod changes the mode of an file not following symlinks.
func (*basicDriver) Lchmod(path string, mode os.FileMode) (err error) {
	// TODO: Use Window's equivalent
	return os.Chmod(path, mode)
}
