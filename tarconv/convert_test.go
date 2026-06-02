package tarconv_test

import (
	"archive/tar"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"testing"
	"time"

	erofs "github.com/erofs/go-erofs"

	"github.com/containerd/continuity/tarconv"
)

// ----------------------------------------------------------------------------
// Helpers
// ----------------------------------------------------------------------------

// buf is a simple in-memory io.WriteSeeker.
type buf struct {
	b   []byte
	off int
}

func (b *buf) Write(p []byte) (int, error) {
	end := b.off + len(p)
	if end > len(b.b) {
		b.b = append(b.b, make([]byte, end-len(b.b))...)
	}
	copy(b.b[b.off:], p)
	b.off = end
	return len(p), nil
}

func (b *buf) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = int64(b.off) + offset
	case io.SeekEnd:
		abs = int64(len(b.b)) + offset
	}
	if abs < 0 {
		return 0, errors.New("negative seek")
	}
	b.off = int(abs)
	return abs, nil
}

func (b *buf) ReadAt(p []byte, off int64) (int, error) {
	if int(off) >= len(b.b) {
		return 0, io.EOF
	}
	n := copy(p, b.b[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// makeTar builds an in-memory tar stream from entries defined by f.
func makeTar(t testing.TB, f func(tw *tar.Writer)) []byte {
	t.Helper()
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	f(tw)
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return out.Bytes()
}

// buildImage applies a single tar layer using the default (convert-whiteouts) mode.
func buildImage(t *testing.T, tarData []byte) []byte {
	t.Helper()
	out := &buf{}
	w := erofs.Create(out)
	if err := tarconv.Apply(w, bytes.NewReader(tarData)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}
	return out.b
}

// buildMergedImage applies layers in order using WithMerge and returns the final image.
func buildMergedImage(t *testing.T, layers ...[]byte) []byte {
	t.Helper()
	out := &buf{}
	w := erofs.Create(out)
	for i, layer := range layers {
		if err := tarconv.Apply(w, bytes.NewReader(layer), tarconv.WithMerge()); err != nil {
			t.Fatalf("Apply(WithMerge) layer %d: %v", i, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}
	return out.b
}

// openImage opens an EROFS image from bytes for reading.
func openImage(t *testing.T, data []byte) fs.FS {
	t.Helper()
	img, err := erofs.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("erofs.Open: %v", err)
	}
	return img
}

// checkFile verifies a file's content.
func checkFile(t *testing.T, fsys fs.FS, name, want string) {
	t.Helper()
	got, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", name, err)
	}
	if string(got) != want {
		t.Errorf("%s: got %q want %q", name, got, want)
	}
}

// checkStat retrieves stat for name.
func checkStat(t *testing.T, fsys fs.FS, name string) fs.FileInfo {
	t.Helper()
	info, err := fs.Stat(fsys, name)
	if err != nil {
		t.Fatalf("Stat %s: %v", name, err)
	}
	return info
}

// checkNotExist asserts the path does not exist.
func checkNotExist(t *testing.T, fsys fs.FS, name string) {
	t.Helper()
	_, err := fs.Stat(fsys, name)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("%s should not exist but Stat returned: %v", name, err)
	}
}

// checkDirNames asserts a directory has exactly the given child names.
func checkDirNames(t *testing.T, fsys fs.FS, dir string, want ...string) {
	t.Helper()
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	got := make(map[string]bool)
	for _, e := range entries {
		got[e.Name()] = true
	}
	wantMap := make(map[string]bool)
	for _, n := range want {
		wantMap[n] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("%s: missing child %q", dir, n)
		}
	}
	for n := range got {
		if !wantMap[n] {
			t.Errorf("%s: unexpected child %q", dir, n)
		}
	}
}

// fsckImage runs fsck.erofs if available.
func fsckImage(t *testing.T, data []byte) {
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

var epoch = time.Unix(1700000000, 0)

// ----------------------------------------------------------------------------
// Convert tests
// ----------------------------------------------------------------------------

// TestConvertBasicFiles exercises a simple tar with files and directories.
func TestConvertBasicFiles(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "etc/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/hostname", Size: 10, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("myhost\n   "))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/passwd", Size: 5, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("root\n"))
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	checkFile(t, fsys, "etc/hostname", "myhost\n   ")
	checkFile(t, fsys, "etc/passwd", "root\n")
	info := checkStat(t, fsys, "etc")
	if !info.IsDir() {
		t.Error("etc should be a directory")
	}
}

// TestConvertMetadata checks uid/gid/mtime/mode are preserved.
func TestConvertMetadata(t *testing.T) {
	mt := time.Unix(1600000000, 123456789)
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg,
			Name:     "secret",
			Size:     3,
			Mode:     0o600,
			Uid:      1000,
			Gid:      2000,
			ModTime:  mt,
		})
		tw.Write([]byte("abc"))
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	info := checkStat(t, fsys, "secret")
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode: got %o want %o", info.Mode().Perm(), 0o600)
	}
	st, ok := info.Sys().(*erofs.Stat)
	if !ok {
		t.Fatalf("Sys() is %T, want *erofs.Stat", info.Sys())
	}
	if st.UID != 1000 {
		t.Errorf("uid: got %d want 1000", st.UID)
	}
	if st.GID != 2000 {
		t.Errorf("gid: got %d want 2000", st.GID)
	}
	if st.Mtime != uint64(mt.Unix()) {
		t.Errorf("mtime: got %d want %d", st.Mtime, mt.Unix())
	}
}

// TestConvertSymlink checks symlinks are preserved.
func TestConvertSymlink(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "usr/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "usr/bin/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/sh", Size: 4, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("#!/s"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "bin", Linkname: "usr/bin", Mode: 0o777, ModTime: epoch})
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	imgFS := openImage(t, img)
	// Use Lstat to avoid following the symlink.
	lstater, ok := imgFS.(interface{ Lstat(string) (fs.FileInfo, error) })
	if !ok {
		t.Skip("image FS does not implement Lstat")
	}
	info, err := lstater.Lstat("bin")
	if err != nil {
		t.Fatalf("Lstat bin: %v", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("bin: expected symlink, got %v", info.Mode())
	}
}

// TestConvertHardLinks exercises in-order hard links.
func TestConvertHardLinks(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "data", Size: 5, Mode: 0o644, ModTime: epoch, Uid: 100})
		tw.Write([]byte("hello"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "data-link", Linkname: "data", ModTime: epoch, Uid: 100})
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	checkFile(t, fsys, "data", "hello")
	checkFile(t, fsys, "data-link", "hello")
	// Verify shared inode (nlink >= 2).
	info, _ := fs.Stat(fsys, "data")
	st := info.Sys().(*erofs.Stat)
	if st.Nlink < 2 {
		t.Errorf("data: nlink = %d, want >= 2", st.Nlink)
	}
}

// TestConvertHardLinksOutOfOrder exercises hard links that appear before their
// target in the tar stream.
func TestConvertHardLinksOutOfOrder(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		// Hard link appears BEFORE the target.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "early-link", Linkname: "actual", ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "actual", Size: 4, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("data"))
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	checkFile(t, fsys, "actual", "data")
	checkFile(t, fsys, "early-link", "data")
	info, _ := fs.Stat(fsys, "actual")
	st := info.Sys().(*erofs.Stat)
	if st.Nlink < 2 {
		t.Errorf("actual: nlink = %d, want >= 2", st.Nlink)
	}
}

// TestConvertUnresolvedHardLink verifies that a hard link whose target never
// appears returns an error.
func TestConvertUnresolvedHardLink(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "broken", Linkname: "ghost", ModTime: epoch})
	})
	out := &buf{}
	w := erofs.Create(out)
	err := tarconv.Apply(w, bytes.NewReader(tarData))
	if err == nil {
		t.Fatal("expected error for unresolved hard link, got nil")
	}
}

// TestConvertDeviceNodes checks char and block devices.
func TestConvertDeviceNodes(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeChar, Name: "dev/null",
			Mode: 0o666, Devmajor: 1, Devminor: 3, ModTime: epoch,
		})
		tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeBlock, Name: "dev/sda",
			Mode: 0o660, Devmajor: 8, Devminor: 0, ModTime: epoch,
		})
		tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeFifo, Name: "tmp/pipe",
			Mode: 0o644, ModTime: epoch,
		})
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)

	info := checkStat(t, fsys, "dev/null")
	if info.Mode()&(fs.ModeDevice|fs.ModeCharDevice) != fs.ModeDevice|fs.ModeCharDevice {
		t.Errorf("dev/null: mode %v should be char device", info.Mode())
	}
	st := info.Sys().(*erofs.Stat)
	// rdev encodes major/minor; just verify it's nonzero for a known device.
	if st.Rdev == 0 {
		t.Errorf("dev/null: rdev should be nonzero")
	}

	info = checkStat(t, fsys, "dev/sda")
	if info.Mode()&fs.ModeDevice == 0 || info.Mode()&fs.ModeCharDevice != 0 {
		t.Errorf("dev/sda: mode %v should be block device", info.Mode())
	}

	info = checkStat(t, fsys, "tmp/pipe")
	if info.Mode()&fs.ModeNamedPipe == 0 {
		t.Errorf("tmp/pipe: mode %v should be named pipe", info.Mode())
	}
}

// TestConvertXattrs checks PAX xattrs survive the round-trip.
func TestConvertXattrs(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg,
			Name:     "bin/ping",
			Size:     4,
			Mode:     0o755,
			ModTime:  epoch,
			PAXRecords: map[string]string{
				"SCHILY.xattr.security.capability": "AQIDBA==",
				"SCHILY.xattr.user.comment":        "hello",
			},
		}
		tw.WriteHeader(hdr)
		tw.Write([]byte("ping"))
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	info := checkStat(t, fsys, "bin/ping")
	st, ok := info.Sys().(*erofs.Stat)
	if !ok {
		t.Fatalf("Sys() is %T", info.Sys())
	}
	if st.Xattrs["security.capability"] != "AQIDBA==" {
		t.Errorf("security.capability: got %q", st.Xattrs["security.capability"])
	}
	if st.Xattrs["user.comment"] != "hello" {
		t.Errorf("user.comment: got %q", st.Xattrs["user.comment"])
	}
}

// TestConvertWhiteouts checks that whiteout entries become overlayfs char
// device 0/0 entries (Convert mode).
func TestConvertWhiteouts(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		// Create the directory so the opaque xattr has somewhere to land.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, ModTime: epoch})
		// Opaque whiteout on lib/.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh..wh..opq", Size: 0, ModTime: epoch})
		// Regular whiteout: removes lib/removed.so.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh.removed.so", Size: 0, ModTime: epoch})
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)

	// lib/removed.so should exist as a char device 0/0.
	info := checkStat(t, fsys, "lib/removed.so")
	if info.Mode()&(fs.ModeDevice|fs.ModeCharDevice) != fs.ModeDevice|fs.ModeCharDevice {
		t.Errorf("lib/removed.so: expected char device whiteout, got mode %v", info.Mode())
	}
	st := info.Sys().(*erofs.Stat)
	if st.Rdev != 0 {
		t.Errorf("lib/removed.so: rdev should be 0 for whiteout, got %d", st.Rdev)
	}

	// lib itself should have trusted.overlay.opaque=y (from .wh..wh..opq) and
	// trusted.overlay.origin="" (from the regular .wh.removed.so whiteout).
	info = checkStat(t, fsys, "lib")
	st = info.Sys().(*erofs.Stat)
	if st.Xattrs[overlayOpaqueXattr] != "y" {
		t.Errorf("lib: expected opaque xattr, got xattrs=%v", st.Xattrs)
	}
	if _, ok := st.Xattrs["trusted.overlay.origin"]; !ok {
		t.Errorf("lib: expected trusted.overlay.origin from regular whiteout, got xattrs=%v", st.Xattrs)
	}
}

// TestConvertOpaqueBeforeDir tests that the opaque xattr is applied even when
// the .wh..wh..opq entry appears before the directory entry itself.
func TestConvertOpaqueBeforeDir(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		// opaque marker BEFORE the directory entry.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "newdir/.wh..wh..opq", Size: 0, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "newdir/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "newdir/file.txt", Size: 3, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("hi!"))
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)

	info := checkStat(t, fsys, "newdir")
	st := info.Sys().(*erofs.Stat)
	if st.Xattrs[overlayOpaqueXattr] != "y" {
		t.Errorf("newdir: expected opaque xattr, got xattrs=%v", st.Xattrs)
	}
	// Opaque directories get trusted.overlay.opaque=y only (not origin).
	// trusted.overlay.origin is set on directories containing regular whiteouts.
	if _, ok := st.Xattrs["trusted.overlay.origin"]; ok {
		t.Errorf("newdir: unexpected trusted.overlay.origin on opaque dir, xattrs=%v", st.Xattrs)
	}
	checkFile(t, fsys, "newdir/file.txt", "hi!")
}

// TestConvertEmptyFile verifies empty regular files work.
func TestConvertEmptyFile(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "empty", Size: 0, Mode: 0o644, ModTime: epoch})
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	checkFile(t, fsys, "empty", "")
}

// TestConvertLargeFile exercises a file that spans multiple EROFS blocks.
func TestConvertLargeFile(t *testing.T) {
	const size = 4*4096 + 7
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i)
	}
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "big", Size: size, Mode: 0o644, ModTime: epoch})
		tw.Write(data)
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	got, err := fs.ReadFile(fsys, "big")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("large file content mismatch: got %d bytes, want %d", len(got), len(data))
	}
}

// TestConvertSetuidBit verifies that setuid/setgid/sticky bits survive.
func TestConvertSetuidBit(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "su", Size: 2, Mode: 0o4755, ModTime: epoch})
		tw.Write([]byte("su"))
	})
	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)
	info := checkStat(t, fsys, "su")
	// erofs.Stat carries raw unix mode; check setuid via Sys().
	st, ok := info.Sys().(*erofs.Stat)
	if !ok {
		t.Fatalf("Sys() is %T, want *erofs.Stat", info.Sys())
	}
	// In Go's fs.FileMode, ModeSetuid is set when the unix setuid bit is present.
	// erofs.Stat.Mode is a Go fs.FileMode.
	if st.Mode&fs.ModeSetuid == 0 {
		t.Errorf("su: setuid bit missing, mode=%v", st.Mode)
	}
}

// ----------------------------------------------------------------------------
// Merge tests
// ----------------------------------------------------------------------------

// TestMergeBasic applies two layers and checks the final state.
func TestMergeBasic(t *testing.T) {
	layer1 := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "etc/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/hosts", Size: 9, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("127.0.0.1"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/passwd", Size: 4, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("root"))
	})
	layer2 := makeTar(t, func(tw *tar.Writer) {
		// Overwrite hosts.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/hosts", Size: 9, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("127.0.0.2"))
		// Whiteout passwd.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/.wh.passwd", Size: 0, ModTime: epoch})
	})
	img := buildMergedImage(t, layer1, layer2)
	fsckImage(t, img)
	fsys := openImage(t, img)
	checkFile(t, fsys, "etc/hosts", "127.0.0.2")
	checkNotExist(t, fsys, "etc/passwd")
}

// TestMergeOpaqueDir checks that .wh..wh..opq removes existing children in
// Merge mode. The merged image must be a clean flattened result: no overlay
// xattrs (trusted.overlay.opaque, trusted.overlay.origin) should appear
// anywhere, and only the upper layer's children should remain.
func TestMergeOpaqueDir(t *testing.T) {
	layer1 := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/libc.so", Size: 4, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("libc"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/libm.so", Size: 4, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("libm"))
	})
	layer2 := makeTar(t, func(tw *tar.Writer) {
		// Opaque: clear lib's children, then add only the new lib.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh..wh..opq", Size: 0, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/libz.so", Size: 4, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("libz"))
	})
	img := buildMergedImage(t, layer1, layer2)
	fsckImage(t, img)
	fsys := openImage(t, img)

	// Old children must be gone.
	checkNotExist(t, fsys, "lib/libc.so")
	checkNotExist(t, fsys, "lib/libm.so")

	// New child must be present with correct content.
	checkFile(t, fsys, "lib/libz.so", "libz")

	// lib/ must not carry any overlay xattrs — the merged image is flat.
	info := checkStat(t, fsys, "lib")
	st := info.Sys().(*erofs.Stat)
	if v, ok := st.Xattrs[overlayOpaqueXattr]; ok {
		t.Errorf("lib: Merge should not leave %q=%q in merged image", overlayOpaqueXattr, v)
	}
	if v, ok := st.Xattrs["trusted.overlay.origin"]; ok {
		t.Errorf("lib: Merge should not leave trusted.overlay.origin=%q in merged image", v)
	}
}

// TestMergeOpaqueDeeplyNested verifies that an opaque marker on a directory
// removes all descendants at every depth, not just direct children.
func TestMergeOpaqueDeeplyNested(t *testing.T) {
	layer1 := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "app/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "app/a/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "app/a/b/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "app/a/b/deep.txt", Size: 4, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("deep"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "app/a/mid.txt", Size: 3, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("mid"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "app/top.txt", Size: 3, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("top"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "app/link", Linkname: "a/mid.txt", ModTime: epoch})
	})
	layer2 := makeTar(t, func(tw *tar.Writer) {
		// Opaque wipes every descendant of app/.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "app/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "app/.wh..wh..opq", Size: 0, ModTime: epoch})
		// Only newfile.txt from this layer should be present.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "app/newfile.txt", Size: 3, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("new"))
	})
	img := buildMergedImage(t, layer1, layer2)
	fsckImage(t, img)
	fsys := openImage(t, img)

	// All layer-1 descendants must be gone — including multi-level nesting.
	checkNotExist(t, fsys, "app/top.txt")
	checkNotExist(t, fsys, "app/link")
	checkNotExist(t, fsys, "app/a")
	checkNotExist(t, fsys, "app/a/mid.txt")
	checkNotExist(t, fsys, "app/a/b")
	checkNotExist(t, fsys, "app/a/b/deep.txt")

	// Layer-2 content must be present.
	checkFile(t, fsys, "app/newfile.txt", "new")

	// No overlay xattrs on the merged directory.
	info := checkStat(t, fsys, "app")
	st := info.Sys().(*erofs.Stat)
	if v, ok := st.Xattrs[overlayOpaqueXattr]; ok {
		t.Errorf("app: Merge should not leave %q=%q in merged image", overlayOpaqueXattr, v)
	}
}

// TestMergeOpaqueNoXattrs verifies that neither regular whiteouts nor opaque
// markers leave any overlay xattrs in the merged image. Merge mode produces a
// flat filesystem; xattrs are an overlay-layer concept that belongs only in
// Convert mode output.
func TestMergeOpaqueNoXattrs(t *testing.T) {
	layer1 := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "etc/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/old.conf", Size: 4, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("old!"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/keep.conf", Size: 4, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("keep"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/old.so", Size: 3, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("old"))
	})
	layer2 := makeTar(t, func(tw *tar.Writer) {
		// Regular whiteout removes etc/old.conf.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/.wh.old.conf", Size: 0, ModTime: epoch})
		// Opaque wipes lib/ entirely and replaces with new.so.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh..wh..opq", Size: 0, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/new.so", Size: 3, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("new"))
	})
	img := buildMergedImage(t, layer1, layer2)
	fsckImage(t, img)
	fsys := openImage(t, img)

	// Structural assertions: correct merge behaviour.
	checkNotExist(t, fsys, "etc/old.conf")
	checkFile(t, fsys, "etc/keep.conf", "keep")
	checkNotExist(t, fsys, "lib/old.so")
	checkFile(t, fsys, "lib/new.so", "new")

	// Walk the entire image and assert no overlay xattrs exist anywhere.
	overlayXattrs := []string{overlayOpaqueXattr, "trusted.overlay.origin", "trusted.overlay.whiteout"}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*erofs.Stat)
		if !ok {
			return nil
		}
		for _, key := range overlayXattrs {
			if v, found := st.Xattrs[key]; found {
				t.Errorf("%s: Merge left overlay xattr %q=%q in merged image", p, key, v)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
}

// TestMergeWhiteoutMissingPath checks that whiteouts targeting non-existent
// paths are silently ignored in Merge mode.
func TestMergeWhiteoutMissingPath(t *testing.T) {
	layer1 := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "exists", Size: 2, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("ok"))
	})
	layer2 := makeTar(t, func(tw *tar.Writer) {
		// Whiteout for a path that was never created.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: ".wh.ghost", Size: 0, ModTime: epoch})
	})
	img := buildMergedImage(t, layer1, layer2)
	fsckImage(t, img)
	fsys := openImage(t, img)
	// Existing file should still be present.
	checkFile(t, fsys, "exists", "ok")
}

// TestMergeHardLinks exercises hard links across a merged image.
func TestMergeHardLinks(t *testing.T) {
	layer1 := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "bin/sh", Size: 5, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("shell"))
	})
	layer2 := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "bin/bash", Linkname: "bin/sh", ModTime: epoch})
	})
	img := buildMergedImage(t, layer1, layer2)
	fsckImage(t, img)
	fsys := openImage(t, img)
	checkFile(t, fsys, "bin/sh", "shell")
	checkFile(t, fsys, "bin/bash", "shell")
	info, _ := fs.Stat(fsys, "bin/sh")
	st := info.Sys().(*erofs.Stat)
	if st.Nlink < 2 {
		t.Errorf("bin/sh: nlink = %d, want >= 2", st.Nlink)
	}
}

// TestMergeThreeLayers tests a three-layer scenario similar to real container
// images (base + deps + app).
func TestMergeThreeLayers(t *testing.T) {
	// Layer 1: base OS skeleton.
	base := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "bin/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "bin/sh", Size: 2, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("sh"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "etc/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/os-release", Size: 6, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("alpine"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "usr/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "usr/lib/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "usr/lib/libc.so", Size: 4, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("libc"))
	})

	// Layer 2: install a package (adds files, removes some base files).
	deps := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "usr/bin/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/python3", Size: 6, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("python"))
		// Remove bin/sh (replaced later by a symlink).
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "bin/.wh.sh", Size: 0, ModTime: epoch})
	})

	// Layer 3: app layer.
	app := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "app/", Mode: 0o755, ModTime: epoch})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "app/main.py", Size: 4, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("main"))
		// Re-add sh as a symlink.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "bin/sh", Linkname: "/bin/busybox", Mode: 0o777, ModTime: epoch})
	})

	img := buildMergedImage(t, base, deps, app)
	fsckImage(t, img)
	fsys := openImage(t, img)

	// etc/os-release is from layer 1 and should still be present.
	checkFile(t, fsys, "etc/os-release", "alpine")
	checkFile(t, fsys, "usr/bin/python3", "python")
	checkFile(t, fsys, "app/main.py", "main")

	// bin/sh was removed in layer 2 and replaced by a symlink in layer 3.
	// Use Lstat to see the symlink itself.
	lstater, ok := fsys.(interface{ Lstat(string) (fs.FileInfo, error) })
	if !ok {
		t.Skip("image FS does not implement Lstat")
	}
	info, err := lstater.Lstat("bin/sh")
	if err != nil {
		t.Fatalf("Lstat bin/sh: %v", err)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Errorf("bin/sh: expected symlink, got %v", info.Mode())
	}
}

// TestConvertNoTempFile verifies that Convert itself does not create a temp
// file for payload data. We set TMPDIR to a read-only dir and verify that
// Convert still succeeds (meaning it doesn't need TMPDIR for its own
// intermediate data). Note: erofs.Writer may create a spool file via
// WithTempDir; we are only verifying Convert's own behaviour, so we pass a
// writable tempDir to the writer explicitly.
func TestConvertNoTempFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root, cannot test read-only tmpdir")
	}
	readonly, err := os.MkdirTemp("", "ro-tmpdir-*")
	if err != nil {
		t.Skip("cannot create temp dir:", err)
	}
	defer os.RemoveAll(readonly)
	if err := os.Chmod(readonly, 0o500); err != nil {
		t.Skip("cannot chmod temp dir:", err)
	}
	// Make a separate writable temp dir for the writer spool.
	writable, err := os.MkdirTemp("", "rw-tmpdir-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(writable)

	t.Setenv("TMPDIR", readonly)

	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "f", Size: 3, Mode: 0o644, ModTime: epoch})
		tw.Write([]byte("abc"))
	})
	out := &buf{}
	w := erofs.Create(out, erofs.WithTempDir(writable))
	if err := tarconv.Apply(w, bytes.NewReader(tarData)); err != nil {
		t.Fatalf("Convert failed: %v", err)
	}
	_ = w.Close()
}

// ----------------------------------------------------------------------------
// Real-image-shape test: simulates Ubuntu base layer structure
// ----------------------------------------------------------------------------

// TestConvertUbuntuLikeLayer exercises a tar that resembles a real Ubuntu
// base layer: deep directory tree, many files, symlinks, a few device nodes.
func TestConvertUbuntuLikeLayer(t *testing.T) {
	tarData := makeTar(t, func(tw *tar.Writer) {
		dirs := []string{"bin/", "sbin/", "lib/", "lib/x86_64-linux-gnu/",
			"etc/", "etc/apt/", "usr/", "usr/bin/", "usr/lib/", "var/", "var/log/",
			"tmp/", "root/", "home/"}
		for _, d := range dirs {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: d, Mode: 0o755, Uid: 0, Gid: 0, ModTime: epoch})
		}
		// Typical binaries.
		for _, f := range []string{"bin/sh", "bin/ls", "bin/cat", "bin/echo", "sbin/init"} {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: f, Size: 8, Mode: 0o755, ModTime: epoch})
			tw.Write([]byte("fakebinx"))
		}
		// Typical libs — hard linked to each other (versioned .so).
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/x86_64-linux-gnu/libc.so.6", Size: 8, Mode: 0o755, ModTime: epoch})
		tw.Write([]byte("libcdata"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "lib/libc.so.6", Linkname: "lib/x86_64-linux-gnu/libc.so.6", ModTime: epoch})
		// Config files.
		for _, f := range []string{"etc/hostname", "etc/hosts", "etc/resolv.conf"} {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: f, Size: 1, Mode: 0o644, Uid: 0, Gid: 0, ModTime: epoch})
			tw.Write([]byte("\n"))
		}
		// Symlinks (common in Ubuntu).
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "lib64", Linkname: "lib/x86_64-linux-gnu", Mode: 0o777, ModTime: epoch})
		// Device node.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeChar, Name: "dev/null", Mode: 0o666, Devmajor: 1, Devminor: 3, ModTime: epoch})
		// File with capability xattr (common for ping, etc.).
		tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg, Name: "usr/bin/ping", Size: 4, Mode: 0o755, ModTime: epoch,
			PAXRecords: map[string]string{"SCHILY.xattr.security.capability": "\x01\x00\x00\x02\x00 \x00\x00"},
		})
		tw.Write([]byte("ping"))
		// Empty log file.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "var/log/dpkg.log", Size: 0, Mode: 0o644, ModTime: epoch})
		// Sticky tmp.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "tmp/", Mode: 0o1777, ModTime: epoch})
	})

	img := buildImage(t, tarData)
	fsckImage(t, img)
	fsys := openImage(t, img)

	checkFile(t, fsys, "bin/sh", "fakebinx")
	checkFile(t, fsys, "lib/x86_64-linux-gnu/libc.so.6", "libcdata")
	checkFile(t, fsys, "lib/libc.so.6", "libcdata") // hard link

	info, _ := fs.Stat(fsys, "lib/x86_64-linux-gnu/libc.so.6")
	st := info.Sys().(*erofs.Stat)
	if st.Nlink < 2 {
		t.Errorf("libc.so.6: nlink=%d want >=2", st.Nlink)
	}

	info = checkStat(t, fsys, "tmp")
	// Sticky bit: check via erofs.Stat.Mode which uses the properly-decoded
	// raw inode mode (fs.FileInfo.Mode() carries it unreliably due to the
	// reader's inode decode path).
	st2 := info.Sys().(*erofs.Stat)
	if st2.Mode.Perm() != 0o777 || st2.Mode&fs.ModeSticky == 0 {
		t.Errorf("tmp: erofs.Stat.Mode=%v want drwxrwxrwt", st2.Mode)
	}

	info = checkStat(t, fsys, "usr/bin/ping")
	st = info.Sys().(*erofs.Stat)
	if st.Xattrs["security.capability"] == "" {
		t.Error("ping: missing security.capability xattr")
	}
}

const overlayOpaqueXattr = "trusted.overlay.opaque"
