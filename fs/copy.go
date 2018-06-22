package fs

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/pkg/errors"
)

// Mode indicates whether to use hardlink or copy content
type Mode int

const (
	// Content creates a new file, and copies the content of the file
	Content Mode = iota
	// Hardlink creates a new hardlink to the existing file
	Hardlink
)

var bufferPool = &sync.Pool{
	New: func() interface{} {
		buffer := make([]byte, 32*1024)
		return &buffer
	},
}

// CopyDir copies the directory from src to dst.
// Most efficient copy of files is attempted.
func CopyDir(dst, src string, copyMode Mode, copyXattrs bool) error {
	inodes := map[uint64]string{}
	return copyDirectory(dst, src, inodes, copyMode, copyXattrs)
}

func copyFileInfo(fi os.FileInfo, name string) error {
	if err := copyFileInfoPhase1(fi, name); err != nil {
		return err
	}
	return copyFileInfoPhase2(fi, name)
}

func copyDirectory(dst, src string, inodes map[uint64]string, copyMode Mode, copyXattrs bool) error {
	stat, err := os.Stat(src)
	if err != nil {
		return errors.Wrapf(err, "failed to stat %s", src)
	}
	if !stat.IsDir() {
		return errors.Errorf("source is not directory")
	}

	if st, err := os.Stat(dst); err != nil {
		if err := os.Mkdir(dst, stat.Mode()); err != nil {
			return errors.Wrapf(err, "failed to mkdir %s", dst)
		}
	} else if !st.IsDir() {
		return errors.Errorf("cannot copy to non-directory: %s", dst)
	} else {
		if err := os.Chmod(dst, stat.Mode()); err != nil {
			return errors.Wrapf(err, "failed to chmod on %s", dst)
		}
	}

	fis, err := ioutil.ReadDir(src)
	if err != nil {
		return errors.Wrapf(err, "failed to read %s", src)
	}

	if err := copyFileInfoPhase1(stat, dst); err != nil {
		return errors.Wrapf(err, "failed to copy file info for %s", dst)
	}

	for _, fi := range fis {
		source := filepath.Join(src, fi.Name())
		target := filepath.Join(dst, fi.Name())

		switch {
		case fi.IsDir():
			if err := copyDirectory(target, source, inodes, copyMode, copyXattrs); err != nil {
				return err
			}
			continue
		case (fi.Mode() & os.ModeType) == 0:
			link, err := getLinkSource(target, fi, inodes)
			if err != nil {
				return errors.Wrap(err, "failed to get hardlink")
			}
			if link == "" && copyMode == Hardlink {
				link = source
			}
			if link != "" {
				if err := os.Link(link, target); err != nil {
					return errors.Wrap(err, "failed to create hard link")
				}
			} else if err := CopyFile(target, source); err != nil {
				return errors.Wrap(err, "failed to copy files")
			}
		case (fi.Mode() & os.ModeSymlink) == os.ModeSymlink:
			link, err := os.Readlink(source)
			if err != nil {
				return errors.Wrapf(err, "failed to read link: %s", source)
			}
			if err := os.Symlink(link, target); err != nil {
				return errors.Wrapf(err, "failed to create symlink: %s", target)
			}
		case (fi.Mode() & os.ModeDevice) == os.ModeDevice:
			if err := copyDevice(target, fi); err != nil {
				return errors.Wrapf(err, "failed to create device")
			}
		default:
			// TODO: Support pipes and sockets
			return errors.Wrapf(err, "unsupported mode %s", fi.Mode())
		}
		if err := copyFileInfoPhase1(fi, target); err != nil {
			return errors.Wrap(err, "failed to copy file info")
		}
		if copyXattrs {
			if err := copyXAttrs(target, source); err != nil {
				return errors.Wrap(err, "failed to copy xattrs")
			}
		}
	}

	if err := copyFileInfoPhase2(stat, dst); err != nil {
		return errors.Wrapf(err, "failed to copy file info for %s", dst)
	}

	return nil
}

// CopyFile copies the source file to the target.
// The most efficient means of copying is used for the platform.
func CopyFile(target, source string) error {
	src, err := os.Open(source)
	if err != nil {
		return errors.Wrapf(err, "failed to open source %s", source)
	}
	defer src.Close()
	tgt, err := os.Create(target)
	if err != nil {
		return errors.Wrapf(err, "failed to open target %s", target)
	}
	defer tgt.Close()

	return copyFileContent(tgt, src)
}
