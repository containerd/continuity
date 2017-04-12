package continuity

import (
	"io"
	"os"
	"syscall"

	"github.com/stevvooe/continuity/sysx"
)

type rawFile interface {
	Fd() uintptr

	// TODO: Add function for getting position of file
}

func fastCopy(dst *os.File, r io.Reader, size int64) (int64, error) {
	rf, ok := r.(rawFile)
	if !ok {
		return 0, nil
	}

	// TODO: track offsets and call multiple times for short writes
	n, err := sysx.CopyFileRange(rf.Fd(), nil, dst.Fd(), nil, int(size), 0)
	if n == 0 && (err == syscall.ENOSYS || err == syscall.EXDEV) {
		// System call not supported
		return 0, nil
	}
	return int64(n), err
}
