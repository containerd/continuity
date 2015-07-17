package continuity

import (
	"os"
	"syscall"
)

// from /usr/include/sys/types.h

func major(dev uint) int {
	return int(uint(dev>>24) & 0xff)
}

func minor(dev uint) int {
	return int(dev & 0xffffff)
}

func makedev(major int, minor int) int {
	return ((major << 24) | minor)
}

// mknod provides a shortcut for syscall.Mknod
func mknod(p string, mode os.FileMode, maj, min int) error {
	var (
		m   = syscallMode(mode.Perm())
		dev int
	)

	if mode&os.ModeDevice != 0 {
		dev = makedev(maj, min)

		if mode&os.ModeCharDevice != 0 {
			m |= syscall.S_IFCHR
		} else {
			m |= syscall.S_IFBLK
		}
	} else if mode&os.ModeNamedPipe != 0 {
		m |= syscall.S_IFIFO
	}

	return syscall.Mknod(p, m, dev)
}

// syscallMode returns the syscall-specific mode bits from Go's portable mode bits.
func syscallMode(i os.FileMode) (o uint32) {
	o |= uint32(i.Perm())
	if i&os.ModeSetuid != 0 {
		o |= syscall.S_ISUID
	}
	if i&os.ModeSetgid != 0 {
		o |= syscall.S_ISGID
	}
	if i&os.ModeSticky != 0 {
		o |= syscall.S_ISVTX
	}
	return
}
