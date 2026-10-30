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

package tarconv_test

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

// writerToTar is satisfied by any type that can emit entries to a tar.Writer.
type writerToTar interface {
	writeTo(tw *tar.Writer) error
}

// tarAll sequences multiple writerToTar entries.
type tarAll []writerToTar

func (a tarAll) writeTo(tw *tar.Writer) error {
	for _, w := range a {
		if err := w.writeTo(tw); err != nil {
			return err
		}
	}
	return nil
}

// tarFromWriterTo returns an io.ReadCloser streaming a tar built from wt.
func tarFromWriterTo(wt writerToTar) io.ReadCloser {
	r, w := io.Pipe()
	go func() {
		tw := tar.NewWriter(w)
		if err := wt.writeTo(tw); err != nil {
			_ = w.CloseWithError(err)
			return
		}
		_ = tw.Close()
		_ = w.Close()
	}()
	return r
}

// tarContext holds shared metadata for generated entries.
type tarContext struct {
	uid     int
	gid     int
	modTime time.Time
	xattrs  map[string]string
}

// --- tarFile ---

type tarFile struct {
	name    string
	data    []byte
	mode    int64
	uid     int
	gid     int
	modTime time.Time
	xattrs  map[string]string
}

func (f *tarFile) writeTo(tw *tar.Writer) error {
	hdr := &tar.Header{
		Typeflag: tar.TypeReg, Name: f.name,
		Size: int64(len(f.data)), Mode: f.mode,
		Uid: f.uid, Gid: f.gid, ModTime: f.modTime,
	}
	if len(f.xattrs) > 0 {
		hdr.PAXRecords = make(map[string]string)
		for k, v := range f.xattrs {
			hdr.PAXRecords["SCHILY.xattr."+k] = v
		}
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if len(f.data) > 0 {
		_, err := tw.Write(f.data)
		return err
	}
	return nil
}

func (tc tarContext) file(name string, data []byte, mode int64) writerToTar {
	return &tarFile{name: name, data: data, mode: mode, uid: tc.uid, gid: tc.gid, modTime: tc.modTime, xattrs: tc.xattrs}
}

// --- tarDir ---

type tarDir struct {
	name    string
	mode    int64
	uid     int
	gid     int
	modTime time.Time
	xattrs  map[string]string
}

func (d *tarDir) writeTo(tw *tar.Writer) error {
	hdr := &tar.Header{
		Typeflag: tar.TypeDir, Name: d.name, Mode: d.mode,
		Uid: d.uid, Gid: d.gid, ModTime: d.modTime,
	}
	if len(d.xattrs) > 0 {
		hdr.PAXRecords = make(map[string]string)
		for k, v := range d.xattrs {
			hdr.PAXRecords["SCHILY.xattr."+k] = v
		}
	}
	return tw.WriteHeader(hdr)
}

func (tc tarContext) dir(name string, mode int64) writerToTar {
	return &tarDir{name: name, mode: mode, uid: tc.uid, gid: tc.gid, modTime: tc.modTime, xattrs: tc.xattrs}
}

// --- tarSymlink ---

type tarSymlink struct {
	name    string
	target  string
	uid     int
	gid     int
	modTime time.Time
}

func (s *tarSymlink) writeTo(tw *tar.Writer) error {
	return tw.WriteHeader(&tar.Header{
		Typeflag: tar.TypeSymlink, Name: s.name, Linkname: s.target,
		Mode: 0o777, Uid: s.uid, Gid: s.gid, ModTime: s.modTime,
	})
}

func (tc tarContext) symlink(name, target string) writerToTar {
	return &tarSymlink{name: name, target: target, uid: tc.uid, gid: tc.gid, modTime: tc.modTime}
}

// --- mkfs.erofs helper ---

// convertTarMkfs runs mkfs.erofs to convert a tar to an EROFS image.
// The flags --tar=f --aufs --quiet -Enoinline_data are always applied.
// Returns an error (and skips) if mkfs.erofs is not found in PATH.
func convertTarMkfs(ctx context.Context, t testing.TB, tarData []byte, outPath string, extraArgs []string) error {
	t.Helper()
	if _, err := exec.LookPath("mkfs.erofs"); err != nil {
		t.Skip("mkfs.erofs not found in PATH")
	}
	f, err := os.CreateTemp("", "mkfs-bench-*.tar")
	if err != nil {
		return fmt.Errorf("create temp tar: %w", err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(tarData); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return err
	}

	args := []string{"--tar=f", "--aufs", "--quiet", "-Enoinline_data"}
	args = append(args, extraArgs...)
	args = append(args, outPath)
	cmd := exec.CommandContext(ctx, "mkfs.erofs", args...)
	cmd.Stdin = f
	out, err := cmd.CombinedOutput()
	_ = f.Close()
	if err != nil {
		return fmt.Errorf("mkfs.erofs %v: %w\n%s", args, err, out)
	}
	return nil
}

// fsckErofsBytes validates an EROFS image using fsck.erofs if available.
func fsckErofsBytes(t testing.TB, data []byte) {
	t.Helper()
	if _, err := exec.LookPath("fsck.erofs"); err != nil {
		return
	}
	f, err := os.CreateTemp("", "erofs-*.img")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()
	out, err := exec.Command("fsck.erofs", f.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("fsck.erofs: %v\n%s", err, out)
	}
}
