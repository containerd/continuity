/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pathdriver

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"github.com/Microsoft/go-winio"
)

// walker is a copy of the standard package filepath.Walk implementation along
// with helper functions, modified to allow visiting folder mounted volumes on Windows.
// Mounting a volume inside a folder is supported on Windows via NTFS reparse points.
// Reparse points allow extending NTFS functionality via a so called filter driver.
// By default, ntfs ships with a number of filter drivers for symlinks (similar to
// symlinks for files on Linux), volume mounts (similar to mounts on Windows),
// directory junction points (similar to bind mounts on Linux), Unix domain sockets
// (https://devblogs.microsoft.com/commandline/af_unix-comes-to-windows/).
//
// Currently go does not walk inside mounted folder, because it treats all reparse points
// as symlinks. See:
// https://go-review.googlesource.com/c/go/+/41830/
//
// The short explanation is that FileMode does not have ModeDir set for any path that also
// has ModeSymlink. ModeSymlink is set on any junction, volume mounts included.
//
// Proper recursion detection in dependent code would have been preferable, along with a
// clear distinction between reparse point types. Until that happens, we need to implement
// this outside of go.
//
// Curretly, this only implements walking into volume mounts, as we only care about those
// at this point. Walking into directory junctions can also be added later, if needed.
type walker struct {
	// volumeToPath holds a mapping between volumes, and paths that we have visited,
	// in which those volumes are mounted. This is used to prevent recursion when
	// walking mounted volumes.
	volumeToPath map[string]string
}

// walk walks the file tree rooted at root, calling fn for each file or
// directory in the tree, including root.
//
// All errors that arise visiting files and directories are filtered by fn:
// see the WalkFunc documentation for details.
//
// The files are walked in lexical order, which makes the output deterministic
// but requires Walk to read an entire directory into memory before proceeding
// to walk that directory.
//
// Walk does not follow symbolic links.
//
// Walk is less efficient than WalkDir, introduced in Go 1.16,
// which avoids calling os.Lstat on every visited file or directory.
func walk(root string, fn filepath.WalkFunc) error {
	w := walker{
		volumeToPath: map[string]string{},
	}
	info, err := os.Lstat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = w.walk(root, info, fn)
	}
	if err == filepath.SkipDir {
		return nil
	}
	return err
}

func (w *walker) walk(path string, info fs.FileInfo, walkFn filepath.WalkFunc) error {
	if info.Mode()&os.ModeSymlink != 0 {
		tgt, err := getReparsePoint(path, info)
		if err != nil {
			return err
		}

		if !tgt.IsMountPoint {
			return walkFn(path, info, nil)
		}

		pth := w.volumeToPath[tgt.Target]
		if pth != "" {
			// This junction was already visited and is one of our parent folders.
			// Send this path to walkFn with an error that denotes a recursion. Allow
			// walkFn to handle the case.
			return walkFn(path, info, fmt.Errorf("%s was already visited: %w", path, ErrPathRecursion))
		}

		// record the volume and the mount point. This will allow us to
		// prevent a recursion if the same volume is mounted again somewhere deeper
		// into our path.
		w.volumeToPath[tgt.Target] = path

		// Remove this volume from the map once we finish walking it.
		// This will allow us to walk it again if it's mounted in another
		// folder, without triggering a recursion.
		defer delete(w.volumeToPath, tgt.Target)
	} else {
		if !info.IsDir() {
			return walkFn(path, info, nil)
		}
	}

	names, err := w.readDirNames(path)
	err1 := walkFn(path, info, err)
	// If err != nil, walk can't walk into this directory.
	// err1 != nil means walkFn want walk to skip this directory or stop walking.
	// Therefore, if one of err and err1 isn't nil, walk will return.
	if err != nil || err1 != nil {
		// The caller's behavior is controlled by the return value, which is decided
		// by walkFn. walkFn may ignore err and return nil.
		// If walkFn returns SkipDir, it will be handled by the caller.
		// So walk should return whatever walkFn returns.
		return err1
	}

	for _, name := range names {
		filename := filepath.Join(path, name)
		fileInfo, err := os.Lstat(filename)
		if err != nil {
			if err := walkFn(filename, fileInfo, err); err != nil && err != filepath.SkipDir {
				return err
			}
		} else {
			err = w.walk(filename, fileInfo, walkFn)
			if err != nil {
				if !fileInfo.IsDir() || err != filepath.SkipDir {
					return err
				}
			}
		}
	}
	return nil
}

func (w *walker) readDirNames(dirname string) ([]string, error) {
	f, err := os.Open(dirname)
	if err != nil {
		return nil, err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

func getFileHandle(path string, info fs.FileInfo) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	attrs := uint32(syscall.FILE_FLAG_BACKUP_SEMANTICS)
	if info.Mode()&fs.ModeSymlink != 0 {
		// Use FILE_FLAG_OPEN_REPARSE_POINT, otherwise CreateFile will follow symlink.
		// See https://docs.microsoft.com/en-us/windows/desktop/FileIO/symbolic-link-effects-on-file-systems-functions#createfile-and-createfiletransacted
		attrs |= syscall.FILE_FLAG_OPEN_REPARSE_POINT
	}
	h, err := syscall.CreateFile(p, 0, 0, nil, syscall.OPEN_EXISTING, attrs, 0)
	if err != nil {
		return 0, err
	}
	return h, nil
}

func readlink(path string, info fs.FileInfo) ([]byte, error) {
	h, err := getFileHandle(path, info)
	if err != nil {
		return nil, err
	}
	defer syscall.CloseHandle(h)

	rdbbuf := make([]byte, syscall.MAXIMUM_REPARSE_DATA_BUFFER_SIZE)
	var bytesReturned uint32
	err = syscall.DeviceIoControl(h, syscall.FSCTL_GET_REPARSE_POINT, nil, 0, &rdbbuf[0], uint32(len(rdbbuf)), &bytesReturned, nil)
	if err != nil {
		return nil, err
	}
	return rdbbuf[:bytesReturned], nil
}

func getReparsePoint(path string, info fs.FileInfo) (*winio.ReparsePoint, error) {
	target, err := readlink(path, info)
	if err != nil {
		return nil, err
	}
	rp, err := winio.DecodeReparsePoint(target)
	if err != nil {
		return nil, err
	}
	return rp, nil
}

// Note that filepath.Walk calls os.Stat, so if the context wants to
// to call Driver.Stat() for Walk, they need to create a new struct that
// overrides this method.
func (*pathDriver) Walk(root string, walkFn filepath.WalkFunc) error {
	return walk(root, walkFn)
}
