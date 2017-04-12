package continuity

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

// atomicWriteFile writes data to a file by first writing to a temp
// file and calling rename.
func atomicWriteFile(filename string, r io.Reader, rf RegularFile) error {
	f, err := ioutil.TempFile(filepath.Dir(filename), ".tmp-"+filepath.Base(filename))
	if err != nil {
		return err
	}
	if err := copyFileContent(f, r, rf.Size()); err != nil {
		f.Close()
		return err
	}
	if err = os.Chmod(f.Name(), rf.Mode()); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

func copyFileContent(dst *os.File, r io.Reader, size int64) error {
	n, err := fastCopy(dst, r, size)
	if err != nil {
		return err
	}
	if n < size {
		if n > 0 {
			return io.ErrShortWrite
		}
		n, err = io.Copy(dst, r)
		if err == nil && n < size {
			return io.ErrShortWrite
		}
		if err != nil {
			return err
		}
	}

	if err := dst.Sync(); err != nil {
		return err
	}

	return nil
}
