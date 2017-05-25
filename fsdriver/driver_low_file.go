package fsdriver

import (
	"os"
)

// Have this for now. Don't actually need these functions
// due to struct embedding, but they're going to be added
// anyway in the real implementation.
type lowFile struct {
	*os.File
}

func (lf *lowFile) Chdir() error {
	return lf.Chdir()
}

func (lf *lowFile) Chmod(mode os.FileMode) error {
	return lf.Chmod(mode)
}

func (lf *lowFile) Chown(uid, gid int) error {
	return lf.Chown(uid, gid)
}

func (lf *lowFile) Close() error {
	return lf.Close()
}

func (lf *lowFile) Fd() uintptr {
	return lf.Fd()
}

func (lf *lowFile) Name() string {
	return lf.Name()
}

func (lf *lowFile) Read(b []byte) (n int, err error) {
	return lf.Read(b)
}

func (lf *lowFile) ReadAt(b []byte, off int64) (n int, err error) {
	return lf.ReadAt(b, off)
}

func (lf *lowFile) Readdir(n int) ([]os.FileInfo, error) {
	return lf.Readdir(n)
}

func (lf *lowFile) Readdirnames(n int) (names []string, err error) {
	return lf.Readdirnames(n)
}

func (lf *lowFile) Seek(offset int64, whence int) (ret int64, err error) {
	return lf.Seek(offset, whence)
}

func (lf *lowFile) Stat() (os.FileInfo, error) {
	return lf.Stat()
}

func (lf *lowFile) Sync() error {
	return lf.Sync()
}

func (lf *lowFile) Truncate(size int64) error {
	return lf.Truncate(size)
}

func (lf *lowFile) Write(b []byte) (n int, err error) {
	return lf.Write(b)
}

func (lf *lowFile) WriteAt(b []byte, off int64) (n int, err error) {
	return lf.WriteAt(b, off)
}

func (lf *lowFile) WriteString(s string) (n int, err error) {
	return lf.WriteString(s)
}
