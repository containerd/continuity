package tarconv_test

// Image comparison tests.
//
// These tests build the same tar with both tar.Convert and mkfs.erofs, then
// walk both images with the go-erofs reader and assert that every entry has
// identical: type, permissions (rawMode), uid, gid, mtime, size, file
// content, symlink target, rdev, and xattrs.
//
// Inode numbers (nid) and block layout are deliberately excluded: they are
// implementation-specific and will legitimately differ.
//
// All builds use a fixed timestamp (-T / WithBuildTime) so mtime values are
// deterministic.

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	erofs "github.com/erofs/go-erofs"

	"github.com/containerd/continuity/tarconv"
)

// fixedBuildTime is used for all comparison builds so mtime is deterministic.
var fixedBuildTime = time.Unix(1700000000, 0)
var fixedBuildTimeStr = "1700000000"

// lstater is the interface for lstat on the erofs image.
type lstater interface {
	Lstat(name string) (fs.FileInfo, error)
}

// readLinker is the interface for reading symlink targets.
type readLinker interface {
	ReadLink(name string) (string, error)
}

// readDirer is the interface for reading directory contents.
type readDirer interface {
	ReadDir(name string) ([]fs.DirEntry, error)
}

// buildGoImage builds an EROFS image using tarconv.Apply (default convert-whiteouts mode).
// The build time is set to fixedBuildTime so compact inodes match mkfs.erofs -T output.
func buildGoImage(t testing.TB, tarData []byte) []byte {
	t.Helper()
	out := &buf{}
	w := erofs.Create(out,
		erofs.WithBuildTime(uint64(fixedBuildTime.Unix()), 0),
	)
	if err := tarconv.Apply(w, bytes.NewReader(tarData)); err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}
	return out.b
}

// buildMkfsImage builds an EROFS image using mkfs.erofs.
// Skips the test if mkfs.erofs is not in PATH.
// Uses --T (fixed build time), --aufs, --tar=f, -Enoinline_data.
func buildMkfsImage(t testing.TB, tarData []byte) []byte {
	t.Helper()
	if _, err := exec.LookPath("mkfs.erofs"); err != nil {
		t.Skip("mkfs.erofs not found in PATH")
	}

	// Write tar to a temp file (mkfs.erofs reads from stdin via --tar=f).
	tarFile, err := os.CreateTemp("", "compare-*.tar")
	if err != nil {
		t.Fatalf("create tar temp: %v", err)
	}
	defer os.Remove(tarFile.Name())
	if _, err := tarFile.Write(tarData); err != nil {
		tarFile.Close()
		t.Fatalf("write tar temp: %v", err)
	}
	if _, err := tarFile.Seek(0, io.SeekStart); err != nil {
		tarFile.Close()
		t.Fatalf("seek tar temp: %v", err)
	}

	outFile, err := os.CreateTemp("", "compare-*.erofs")
	if err != nil {
		tarFile.Close()
		t.Fatalf("create img temp: %v", err)
	}
	outPath := outFile.Name()
	outFile.Close()
	defer os.Remove(outPath)

	// -T sets the EROFS image build time (compact-inode threshold).
	// Do NOT pass --all-time: that would override per-entry mtimes from the tar,
	// causing all entries to show the build time instead of their own mtime.
	args := []string{
		"--tar=f",
		"--aufs",
		"--quiet",
		"-Enoinline_data",
		"-T" + fixedBuildTimeStr,
		outPath,
	}
	cmd := exec.CommandContext(context.Background(), "mkfs.erofs", args...)
	cmd.Stdin = tarFile
	out, err := cmd.CombinedOutput()
	tarFile.Close()
	if err != nil {
		t.Fatalf("mkfs.erofs: %v\n%s", err, out)
	}

	imgBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read mkfs image: %v", err)
	}
	return imgBytes
}

// fsckImage runs fsck.erofs on data (writes to a temp file). Skips if
// fsck.erofs is not in PATH. Calls t.Errorf on failure (not Fatal so other
// checks can run).
func fsckImageBytes(t testing.TB, label string, data []byte) {
	t.Helper()
	if _, err := exec.LookPath("fsck.erofs"); err != nil {
		return
	}
	f, err := os.CreateTemp("", "fsck-*.erofs")
	if err != nil {
		t.Errorf("%s fsck: create temp: %v", label, err)
		return
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		t.Errorf("%s fsck: write: %v", label, err)
		return
	}
	f.Close()
	out, err := exec.Command("fsck.erofs", f.Name()).CombinedOutput()
	if err != nil {
		t.Errorf("%s fsck.erofs FAILED: %v\n%s", label, err, out)
	}
}

// imageEntry is a fully normalized representation of one filesystem entry,
// collected via the go-erofs reader. Every field that can be compared between
// two images derived from the same tar source is stored here.
type imageEntry struct {
	path    string
	rawMode uint16 // Unix mode bits (type + perms + special) from erofs.Stat
	uid     uint32
	gid     uint32
	mtime   uint64
	mtimeNs uint32
	size    int64
	rdev    uint32
	nlink   int    // exact nlink value — must match between images
	symlink string // target for symlinks, empty otherwise
	xattrs  map[string]string
	// dirChildren holds the ordered list of child names as returned by ReadDir.
	// Order matters: EROFS stores directory entries sorted, so both images
	// should report identical order for the same directory contents.
	dirChildren []string
	// content holds the full file data for regular files. Always read in full
	// regardless of size so content correctness is always verified.
	content []byte
}

// collectImage walks an EROFS image opened with erofs.Open and returns a
// sorted slice of imageEntry for every path including the root (".").
//
// It uses Lstat (not Stat) for every entry so symlinks are captured as-is.
// It reads every regular file in full so content is always compared.
// It records the ReadDir order of every directory so ordering is compared.
func collectImage(t testing.TB, img fs.FS, label string) []imageEntry {
	t.Helper()

	ls, ok := img.(lstater)
	if !ok {
		t.Fatalf("%s: image does not implement Lstat", label)
	}
	rl, _ := img.(readLinker)
	rd, ok := img.(readDirer)
	if !ok {
		t.Fatalf("%s: image does not implement ReadDir", label)
	}

	var entries []imageEntry

	// Collect one entry. path is the fs.FS path (relative, no leading slash).
	// "." refers to the root directory.
	collect := func(p string) {
		var fi fs.FileInfo
		var err error
		if p == "." {
			fi, err = ls.Lstat(".")
		} else {
			fi, err = ls.Lstat(p)
		}
		if err != nil {
			t.Errorf("%s Lstat %q: %v", label, p, err)
			return
		}
		st, ok := fi.Sys().(*erofs.Stat)
		if !ok {
			t.Errorf("%s %q: Sys() is %T not *erofs.Stat", label, p, fi.Sys())
			return
		}

		e := imageEntry{
			path:    p,
			rawMode: goModeToRaw(st.Mode),
			uid:     st.UID,
			gid:     st.GID,
			mtime:   st.Mtime,
			mtimeNs: st.MtimeNs,
			size:    fi.Size(),
			rdev:    st.Rdev,
			nlink:   st.Nlink,
			xattrs:  st.Xattrs,
		}

		if fi.Mode()&fs.ModeSymlink != 0 && rl != nil {
			target, err := rl.ReadLink(p)
			if err != nil {
				t.Errorf("%s ReadLink %q: %v", label, p, err)
			}
			e.symlink = target
		}

		if fi.Mode().IsDir() {
			des, err := rd.ReadDir(p)
			if err != nil {
				t.Errorf("%s ReadDir %q: %v", label, p, err)
			} else {
				e.dirChildren = make([]string, len(des))
				for i, de := range des {
					e.dirChildren[i] = de.Name()
				}
			}
		}

		if fi.Mode().IsRegular() && fi.Size() > 0 {
			f, err := img.Open(p)
			if err != nil {
				t.Errorf("%s Open %q: %v", label, p, err)
			} else {
				data, err := io.ReadAll(f)
				f.Close()
				if err != nil {
					t.Errorf("%s ReadAll %q: %v", label, p, err)
				} else {
					e.content = data
				}
			}
		}

		entries = append(entries, e)
	}

	// Walk using fs.WalkDir which uses Stat (follows symlinks for type), but we
	// want to visit symlinks as entries too. Use a manual recursive walk that
	// calls Lstat directly so we see symlinks as-is.
	var walk func(dir string)
	walk = func(dir string) {
		des, err := rd.ReadDir(dir)
		if err != nil {
			t.Errorf("%s ReadDir %q: %v", label, dir, err)
			return
		}
		for _, de := range des {
			var p string
			if dir == "." {
				p = de.Name()
			} else {
				p = dir + "/" + de.Name()
			}
			collect(p)
			// Recurse into real directories only (not symlinks to dirs).
			if de.Type().IsDir() {
				walk(p)
			}
		}
	}

	// Include the root itself.
	collect(".")
	walk(".")

	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	return entries
}

// goModeToRaw converts a Go fs.FileMode (as returned by erofs.Stat.Mode, which
// uses EroFSModeToGoFileMode and correctly carries ModeSetuid/Sticky/etc) back
// to Unix mode bits for comparison. This is the inverse of EroFSModeToGoFileMode.
func goModeToRaw(m fs.FileMode) uint16 {
	var raw uint16
	raw |= uint16(m.Perm())
	if m&fs.ModeSetuid != 0 {
		raw |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		raw |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		raw |= 0o1000
	}
	switch m.Type() {
	case fs.ModeDir:
		raw |= 0o040000
	case fs.ModeSymlink:
		raw |= 0o120000
	case fs.ModeDevice | fs.ModeCharDevice:
		raw |= 0o020000
	case fs.ModeDevice:
		raw |= 0o060000
	case fs.ModeNamedPipe:
		raw |= 0o010000
	case fs.ModeSocket:
		raw |= 0o140000
	default: // regular file
		raw |= 0o100000
	}
	return raw
}

// isDeviceType returns true if rawMode describes a character or block device.
func isDeviceType(rawMode uint16) bool {
	typ := rawMode & 0xF000
	return typ == 0o020000 || typ == 0o060000
}

// compareImages asserts that two EROFS images contain exactly the same
// filesystem: same paths, same metadata on every entry, same file content,
// same directory child order, same xattrs. Differences are reported via
// t.Errorf so all mismatches are collected before the test fails.
func compareImages(t testing.TB, goImg, mkfsImg []byte) {
	t.Helper()

	goFS, err := erofs.Open(bytes.NewReader(goImg))
	if err != nil {
		t.Fatalf("open go image: %v", err)
	}
	mkFS, err := erofs.Open(bytes.NewReader(mkfsImg))
	if err != nil {
		t.Fatalf("open mkfs image: %v", err)
	}

	goEntries := collectImage(t, goFS, "go")
	mkEntries := collectImage(t, mkFS, "mkfs")

	// Build path-keyed maps for fast lookup.
	goMap := make(map[string]imageEntry, len(goEntries))
	for _, e := range goEntries {
		goMap[e.path] = e
	}
	mkMap := make(map[string]imageEntry, len(mkEntries))
	for _, e := range mkEntries {
		mkMap[e.path] = e
	}

	// Every path in go image must exist in mkfs image with identical fields.
	for _, ge := range goEntries {
		me, ok := mkMap[ge.path]
		if !ok {
			t.Errorf("path %q: in go image but missing from mkfs image", ge.path)
			continue
		}
		diffEntries(t, ge.path, ge, me)
	}

	// Every path in mkfs image must exist in go image.
	for _, me := range mkEntries {
		if _, ok := goMap[me.path]; !ok {
			t.Errorf("path %q: in mkfs image but missing from go image", me.path)
		}
	}
}

// diffEntries reports every difference between two imageEntry values for the
// same path. All fields are compared exactly unless noted.
func diffEntries(t testing.TB, p string, got, want imageEntry) {
	t.Helper()

	// Mode: compare full unix bits (type + perms + special bits).
	if got.rawMode != want.rawMode {
		t.Errorf("%s: mode: go=0o%o mkfs=0o%o", p, got.rawMode, want.rawMode)
	}
	if got.uid != want.uid {
		t.Errorf("%s: uid: go=%d mkfs=%d", p, got.uid, want.uid)
	}
	if got.gid != want.gid {
		t.Errorf("%s: gid: go=%d mkfs=%d", p, got.gid, want.gid)
	}
	if got.mtime != want.mtime {
		t.Errorf("%s: mtime: go=%d mkfs=%d", p, got.mtime, want.mtime)
	}
	// mtimeNs: compare only when both are non-zero; mkfs.erofs may not
	// preserve sub-second precision in all versions.
	if got.mtimeNs != 0 && want.mtimeNs != 0 && got.mtimeNs != want.mtimeNs {
		t.Errorf("%s: mtime_ns: go=%d mkfs=%d", p, got.mtimeNs, want.mtimeNs)
	}
	if got.size != want.size {
		t.Errorf("%s: size: go=%d mkfs=%d", p, got.size, want.size)
	}
	if got.symlink != want.symlink {
		t.Errorf("%s: symlink target: go=%q mkfs=%q", p, got.symlink, want.symlink)
	}
	// rdev: compare for device nodes only.
	if isDeviceType(got.rawMode) && got.rdev != want.rdev {
		t.Errorf("%s: rdev: go=%d mkfs=%d", p, got.rdev, want.rdev)
	}
	// nlink: exact comparison. Both images are built from the same tar so every
	// hard-link group must have the same nlink count.
	if got.nlink != want.nlink {
		t.Errorf("%s: nlink: go=%d mkfs=%d", p, got.nlink, want.nlink)
	}
	// xattrs: exact match — same keys, same values, no extras on either side.
	for k, gv := range got.xattrs {
		mv, ok := want.xattrs[k]
		if !ok {
			t.Errorf("%s: xattr %q in go image, absent in mkfs image", p, k)
		} else if gv != mv {
			t.Errorf("%s: xattr %q: go=%q mkfs=%q", p, k, gv, mv)
		}
	}
	for k := range want.xattrs {
		if _, ok := got.xattrs[k]; !ok {
			t.Errorf("%s: xattr %q in mkfs image, absent in go image", p, k)
		}
	}
	// Directory child order: EROFS always stores entries lexicographically, so
	// both images must report the same order.
	if len(got.dirChildren) != len(want.dirChildren) {
		t.Errorf("%s: dir child count: go=%d mkfs=%d (%v vs %v)",
			p, len(got.dirChildren), len(want.dirChildren), got.dirChildren, want.dirChildren)
	} else {
		for i := range got.dirChildren {
			if got.dirChildren[i] != want.dirChildren[i] {
				t.Errorf("%s: dir child[%d]: go=%q mkfs=%q", p, i, got.dirChildren[i], want.dirChildren[i])
			}
		}
	}
	// File content: exact byte comparison.
	if !bytes.Equal(got.content, want.content) {
		n := 64
		if len(got.content) < n {
			n = len(got.content)
		}
		wn := n
		if len(want.content) < wn {
			wn = len(want.content)
		}
		t.Errorf("%s: content mismatch (len go=%d mkfs=%d); go[:%d]=%x mkfs[:%d]=%x",
			p, len(got.content), len(want.content), n, got.content[:n], wn, want.content[:wn])
	}
}

// buildComparisonTar creates a comprehensive deterministic tar that exercises
// every path through tar.Convert and every erofs.Writer call it makes:
//
//   - Directories with varied uid/gid/mtime/mode including sticky bits
//     (forces Mkdir + Chown + Chtimes + Chmod on dirs)
//   - Regular files with varied uid/gid/mtime/mode including setuid/setgid
//     (forces Create + Chown + Chtimes + Chmod on files)
//   - Regular files with PAX xattrs on multiple entry types
//     (forces Setxattr on files, dirs, symlinks, and device nodes)
//   - A 3-way hard-link group (canonical + 2 aliases, nlink=3)
//     (forces Link x2 and exact nlink=3 match)
//   - A 2-way hard-link group in a different directory (cross-dir links)
//   - Symlinks with non-root uid/gid and non-default mtime
//     (forces Chown + Chtimes on symlinks)
//   - An opaque directory (.wh..wh..opq) which must appear in both images
//     as trusted.overlay.opaque=y + trusted.overlay.origin=""
//   - A plain whiteout (.wh.<name>) which must appear as a char device 0/0
//   - Char device (major/minor), block device (major/minor), FIFO
//     (forces Mknod for all three types)
//   - A multi-block file whose content spans more than one EROFS block
//   - An empty regular file
//
// Every directory has an explicit entry in the tar so root metadata is
// deterministic across both converters.
func buildComparisonTar(t testing.TB) []byte {
	t.Helper()

	// Use a single timestamp for all entries. mkfs.erofs 1.9 applies its -T
	// build time to every entry regardless of per-entry tar mtime, so a
	// deterministic comparison requires matching timestamps throughout.
	// Chown/Chmod/Setxattr are verified via uid/gid/mode/xattr fields, not mtime.
	ts := fixedBuildTime // 1700000000

	return makeTar(t, func(tw *tar.Writer) {
		must := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatalf("write tar: %v", err)
			}
		}
		hdr := func(h tar.Header) { must(tw.WriteHeader(&h)) }
		data := func(b []byte) { _, err := tw.Write(b); must(err) }

		// --- Root and top-level directories ---
		// Root: uid=0 gid=0, ts
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})

		// bin/: uid=0 gid=0 — standard mode
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "bin/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})

		// etc/: uid=0 gid=0 — different mtime to exercise Chtimes
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "etc/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})

		// usr/ and usr/bin/ owned by uid=0 gid=0
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "usr/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "usr/bin/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})

		// lib/ and lib/shared/: uid=0, gid=0, different timestamps
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "lib/shared/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})

		// home/: uid=0, gid=0, ts
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "home/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		// home/user/: non-root uid/gid, restricted perms — exercises Chown on dir
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "home/user/", Mode: 0o700, Uid: 1000, Gid: 1000, ModTime: ts})

		// tmp/: sticky bit (0o1777) — exercises Chmod for special bits on dir
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "tmp/", Mode: 0o1777, Uid: 0, Gid: 0, ModTime: ts})

		// var/ and var/log/: gid=4 (adm), exercises Chown with non-standard gid
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "var/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "var/log/", Mode: 0o755, Uid: 0, Gid: 4, ModTime: ts})

		// dev/: uid=0, gid=0 — must be explicit so metadata matches mkfs.erofs
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "dev/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})

		// --- Regular files: varied uid/gid/mtime/mode ---

		// etc/hostname: uid=0, gid=0, ts, 0o644
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "etc/hostname", Size: 8, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
		data([]byte("myhost\n\n"))

		// etc/shadow: uid=0, gid=42 (shadow), ts, 0o640 — Chown with non-root gid
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "etc/shadow", Size: 5, Mode: 0o640, Uid: 0, Gid: 42, ModTime: ts})
		data([]byte("root:"))

		// etc/motd: empty file, different mtime
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "etc/motd", Size: 0, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})

		// bin/sudo: setuid (0o4755) — exercises Chmod for setuid bit
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "bin/sudo", Size: 4, Mode: 0o4755, Uid: 0, Gid: 0, ModTime: ts})
		data([]byte("sudo"))

		// bin/wall: setgid (0o2755), gid=5 (tty) — exercises Chmod for setgid + Chown
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "bin/wall", Size: 4, Mode: 0o2755, Uid: 0, Gid: 5, ModTime: ts})
		data([]byte("wall"))

		// bin/ping: capability xattr + ts + uid=0 gid=0 — exercises Setxattr on regular file
		hdr(tar.Header{
			Typeflag: tar.TypeReg, Name: "bin/ping", Size: 4, Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts,
			PAXRecords: map[string]string{
				"SCHILY.xattr.security.capability": "\x01\x00\x00\x02\x00\x20\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00",
				"SCHILY.xattr.user.role":           "network-tool",
			},
		})
		data([]byte("ping"))

		// usr/bin/env: uid=0 gid=0 ts — plain executable
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "usr/bin/env", Size: 3, Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		data([]byte("env"))

		// home/user/notes.txt: uid=1000 gid=1000 ts — non-root owner + Chtimes
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "home/user/notes.txt", Size: 5, Mode: 0o600, Uid: 1000, Gid: 1000, ModTime: ts})
		data([]byte("hello"))

		// home/user/bigfile: multi-block (>4096 bytes), uid=1000 gid=1000
		bigData := make([]byte, 3*4096+512)
		for i := range bigData { bigData[i] = byte(i % 251) }
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "home/user/bigfile", Size: int64(len(bigData)), Mode: 0o600, Uid: 1000, Gid: 1000, ModTime: ts})
		data(bigData)

		// var/log/syslog: uid=0 gid=4 ts — Chown with adm gid
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "var/log/syslog", Size: 0, Mode: 0o640, Uid: 0, Gid: 4, ModTime: ts})

		// --- Symlinks ---

		// bin/sh → /bin/busybox: uid=0 gid=0 ts
		hdr(tar.Header{Typeflag: tar.TypeSymlink, Name: "bin/sh", Linkname: "/bin/busybox", Mode: 0o777, Uid: 0, Gid: 0, ModTime: ts})

		// etc/localtime → /usr/share/zoneinfo/UTC: uid=0 gid=0 ts — Chtimes on symlink
		hdr(tar.Header{Typeflag: tar.TypeSymlink, Name: "etc/localtime", Linkname: "/usr/share/zoneinfo/UTC", Mode: 0o777, Uid: 0, Gid: 0, ModTime: ts})

		// home/user/link → ../usr/bin/env: non-root uid/gid — Chown on symlink
		hdr(tar.Header{Typeflag: tar.TypeSymlink, Name: "home/user/myenv", Linkname: "../../usr/bin/env", Mode: 0o777, Uid: 1000, Gid: 1000, ModTime: ts})

		// --- Hard links ---

		// 3-way hard-link group: lib/shared/data.bin (canonical) + 2 aliases.
		// nlink must be exactly 3 in both images.
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "lib/shared/data.bin", Size: 8, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
		data([]byte("sharedXX"))
		hdr(tar.Header{Typeflag: tar.TypeLink, Name: "lib/shared/data.bin.1", Linkname: "lib/shared/data.bin", Uid: 0, Gid: 0, ModTime: ts})
		hdr(tar.Header{Typeflag: tar.TypeLink, Name: "lib/shared/data.bin.2", Linkname: "lib/shared/data.bin", Uid: 0, Gid: 0, ModTime: ts})

		// 2-way cross-directory hard link: canonical in etc/, alias in var/log/
		// exercises Link across directory boundaries.
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "etc/group", Size: 6, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
		data([]byte("root:x"))
		hdr(tar.Header{Typeflag: tar.TypeLink, Name: "var/log/group.bak", Linkname: "etc/group", Uid: 0, Gid: 0, ModTime: ts})

		// --- Opaque directory ---
		// app/ is opaque: it contains .wh..wh..opq which signals that any lower-layer
		// contents of app/ are hidden. In Convert mode this sets
		// trusted.overlay.opaque=y and trusted.overlay.origin="" on app/.
		// The directory also has a file so the image is non-trivial.
		hdr(tar.Header{Typeflag: tar.TypeDir, Name: "app/", Mode: 0o755, Uid: 1000, Gid: 1000, ModTime: ts})
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "app/.wh..wh..opq", Size: 0, Uid: 0, Gid: 0, ModTime: ts})
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "app/main", Size: 4, Mode: 0o755, Uid: 1000, Gid: 1000, ModTime: ts})
		data([]byte("main"))

		// --- Plain whiteout ---
		// etc/.wh.removed-file converts to a char device 0/0 (mode 0) at etc/removed-file.
		hdr(tar.Header{Typeflag: tar.TypeReg, Name: "etc/.wh.removed-file", Size: 0, Uid: 0, Gid: 0, ModTime: ts})

		// --- Device nodes (Mknod) ---

		// char device: /dev/null (1,3) — standard whiteout device
		hdr(tar.Header{Typeflag: tar.TypeChar, Name: "dev/null", Mode: 0o666, Uid: 0, Gid: 0, Devmajor: 1, Devminor: 3, ModTime: ts})

		// char device: /dev/zero (1,5)
		hdr(tar.Header{Typeflag: tar.TypeChar, Name: "dev/zero", Mode: 0o666, Uid: 0, Gid: 0, Devmajor: 1, Devminor: 5, ModTime: ts})

		// char device with non-root uid/gid and ts — exercises Chown+Chtimes on mknod
		hdr(tar.Header{Typeflag: tar.TypeChar, Name: "dev/tty1", Mode: 0o620, Uid: 0, Gid: 5, Devmajor: 4, Devminor: 1, ModTime: ts})

		// block device: /dev/sda (8,0) — exercises Mknod with block type
		hdr(tar.Header{Typeflag: tar.TypeBlock, Name: "dev/sda", Mode: 0o660, Uid: 0, Gid: 6, Devmajor: 8, Devminor: 0, ModTime: ts})

		// block device: /dev/sda1 (8,1)
		hdr(tar.Header{Typeflag: tar.TypeBlock, Name: "dev/sda1", Mode: 0o660, Uid: 0, Gid: 6, Devmajor: 8, Devminor: 1, ModTime: ts})

		// FIFO: uid=1000 gid=1000 — exercises Mknod for fifo + Chown
		hdr(tar.Header{Typeflag: tar.TypeFifo, Name: "tmp/pipe", Mode: 0o600, Uid: 1000, Gid: 1000, ModTime: ts})

		// Another FIFO with different permissions — confirms mode bits for fifo
		hdr(tar.Header{Typeflag: tar.TypeFifo, Name: "tmp/ctrl", Mode: 0o640, Uid: 0, Gid: 1000, ModTime: ts})

		// --- Directory with xattrs (SELinux label) ---
		// var/log/ has an xattr, exercising Setxattr on a directory.
		// We set it here by re-emitting var/log/ — the duplicate Mkdir is handled
		// by the idempotent addDir path, and applyMetadata sets the xattr.
		hdr(tar.Header{
			Typeflag: tar.TypeDir, Name: "var/log/", Mode: 0o755, Uid: 0, Gid: 4, ModTime: ts,
			PAXRecords: map[string]string{
				"SCHILY.xattr.security.selinux": "system_u:object_r:var_log_t:s0\x00",
			},
		})
	})
}

// ----------------------------------------------------------------------------
// Comparison tests
// ----------------------------------------------------------------------------

// TestCompareWithMkfs builds the same tar with both tar.Convert and mkfs.erofs
// and asserts the resulting images are semantically identical.
func TestCompareWithMkfs(t *testing.T) {
	tarData := buildComparisonTar(t)
	goImg := buildGoImage(t, tarData)
	mkfsImg := buildMkfsImage(t, tarData)

	fsckImageBytes(t, "go", goImg)
	fsckImageBytes(t, "mkfs", mkfsImg)
	compareImages(t, goImg, mkfsImg)
}

// TestCompareWithMkfsSymlinkDir builds a tar containing a directory that is
// also a symlink target, to exercise the Lstat path.
func TestCompareWithMkfsSymlinkDir(t *testing.T) {
	ts := fixedBuildTime
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "real/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "real/a", Size: 1, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
		tw.Write([]byte("a"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "link", Linkname: "real", Mode: 0o777, Uid: 0, Gid: 0, ModTime: ts})
	})
	goImg := buildGoImage(t, tarData)
	mkfsImg := buildMkfsImage(t, tarData)
	fsckImageBytes(t, "go", goImg)
	fsckImageBytes(t, "mkfs", mkfsImg)
	compareImages(t, goImg, mkfsImg)
}

// TestCompareWithMkfsHardLinksOutOfOrder verifies that go-erofs produces a
// valid image for out-of-order hard links and runs fsck on it. mkfs.erofs 1.9
// does not support hard links whose target appears later in the tar stream
// (it errors with ENOENT), so we only compare against our own image with fsck.
func TestCompareWithMkfsHardLinksOutOfOrder(t *testing.T) {
	ts := fixedBuildTime
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "a/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		// Link before target — mkfs.erofs cannot handle this.
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "a/link", Linkname: "a/target", Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "a/target", Size: 5, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
		tw.Write([]byte("hello"))
	})
	goImg := buildGoImage(t, tarData)
	fsckImageBytes(t, "go", goImg)

	// Verify content via the go-erofs reader.
	imgFS, err := erofs.Open(bytes.NewReader(goImg))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	got, err := fs.ReadFile(imgFS, "a/target")
	if err != nil {
		t.Fatalf("ReadFile a/target: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("a/target: got %q want hello", got)
	}
	got2, err := fs.ReadFile(imgFS, "a/link")
	if err != nil {
		t.Fatalf("ReadFile a/link: %v", err)
	}
	if string(got2) != "hello" {
		t.Errorf("a/link: got %q want hello", got2)
	}
}

// TestCompareWithMkfsWhiteouts builds a tar with OCI whiteout entries.
// mkfs.erofs --aufs converts them to char devices too, so the outputs
// should match.
func TestCompareWithMkfsWhiteouts(t *testing.T) {
	ts := fixedBuildTime
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh.removed.so", Size: 0, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh..wh..opq", Size: 0, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/present.so", Size: 4, Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.Write([]byte("lib!"))
	})
	goImg := buildGoImage(t, tarData)
	mkfsImg := buildMkfsImage(t, tarData)
	fsckImageBytes(t, "go", goImg)
	fsckImageBytes(t, "mkfs", mkfsImg)
	compareImages(t, goImg, mkfsImg)
}

// TestCompareWithMkfsUbuntuLike runs the full Ubuntu-shaped workload through
// both converters and diffs the results.
func TestCompareWithMkfsUbuntuLike(t *testing.T) {
	ts := fixedBuildTime
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		dirs := []string{"bin/", "sbin/", "lib/", "lib/x86_64-linux-gnu/",
			"etc/", "etc/apt/", "usr/", "usr/bin/", "usr/lib/", "var/", "var/log/"}
		for _, d := range dirs {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: d, Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		}
		for _, name := range []string{"bin/sh", "bin/ls", "sbin/init"} {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Size: 4, Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
			tw.Write([]byte("fake"))
		}
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/x86_64-linux-gnu/libc.so.6", Size: 8, Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.Write([]byte("libcdata"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "lib/libc.so.6", Linkname: "lib/x86_64-linux-gnu/libc.so.6", Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "lib64", Linkname: "lib/x86_64-linux-gnu", Mode: 0o777, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "dev/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeChar, Name: "dev/null", Mode: 0o666, Uid: 0, Gid: 0, Devmajor: 1, Devminor: 3, ModTime: ts})
		tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg, Name: "usr/bin/ping", Size: 4, Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts,
			PAXRecords: map[string]string{"SCHILY.xattr.security.capability": "\x01\x00\x00\x02\x00 \x00\x00"},
		})
		tw.Write([]byte("ping"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "var/log/syslog", Size: 0, Mode: 0o640, Uid: 0, Gid: 4, ModTime: ts})
	})

	goImg := buildGoImage(t, tarData)
	mkfsImg := buildMkfsImage(t, tarData)
	fsckImageBytes(t, "go", goImg)
	fsckImageBytes(t, "mkfs", mkfsImg)
	compareImages(t, goImg, mkfsImg)
}

// TestCompareFSWalk is the definitive filesystem equality test.
//
// It builds the same comprehensive tar with both tar.Convert and mkfs.erofs,
// then walks both resulting images as fs.FS from root to leaves and asserts
// exact equality at every node. This goes beyond the targeted metadata checks
// above by also verifying:
//   - total entry count is identical
//   - directory child order is identical (EROFS sorts lexicographically)
//   - every file's full byte content matches
//   - the root directory (".") itself matches
//   - every xattr present on either side is present on the other
//   - nlink is exactly equal (not just ">= 2")
//   - rdev is exactly equal for device nodes
//   - the complete unix mode word (type + special + perm) matches
func TestCompareFSWalk(t *testing.T) {
	tarData := buildComparisonTar(t)
	goImg := buildGoImage(t, tarData)
	mkfsImg := buildMkfsImage(t, tarData)

	// fsck both images first.
	fsckImageBytes(t, "go", goImg)
	fsckImageBytes(t, "mkfs", mkfsImg)

	goFS, err := erofs.Open(bytes.NewReader(goImg))
	if err != nil {
		t.Fatalf("open go image: %v", err)
	}
	mkFS, err := erofs.Open(bytes.NewReader(mkfsImg))
	if err != nil {
		t.Fatalf("open mkfs image: %v", err)
	}

	goEntries := collectImage(t, goFS, "go")
	mkEntries := collectImage(t, mkFS, "mkfs")

	// The sorted entry slices must have the same length.
	if len(goEntries) != len(mkEntries) {
		t.Errorf("entry count mismatch: go=%d mkfs=%d", len(goEntries), len(mkEntries))
		// Still print which paths differ.
		goSet := make(map[string]bool, len(goEntries))
		for _, e := range goEntries {
			goSet[e.path] = true
		}
		mkSet := make(map[string]bool, len(mkEntries))
		for _, e := range mkEntries {
			mkSet[e.path] = true
		}
		for _, e := range goEntries {
			if !mkSet[e.path] {
				t.Errorf("  go-only path: %q", e.path)
			}
		}
		for _, e := range mkEntries {
			if !goSet[e.path] {
				t.Errorf("  mkfs-only path: %q", e.path)
			}
		}
	}

	// Walk in parallel sorted order and compare entry by entry.
	i, j := 0, 0
	for i < len(goEntries) && j < len(mkEntries) {
		ge := goEntries[i]
		me := mkEntries[j]
		switch {
		case ge.path == me.path:
			diffEntries(t, ge.path, ge, me)
			i++
			j++
		case ge.path < me.path:
			t.Errorf("path %q: in go image only", ge.path)
			i++
		default:
			t.Errorf("path %q: in mkfs image only", me.path)
			j++
		}
	}
	for ; i < len(goEntries); i++ {
		t.Errorf("path %q: in go image only (tail)", goEntries[i].path)
	}
	for ; j < len(mkEntries); j++ {
		t.Errorf("path %q: in mkfs image only (tail)", mkEntries[j].path)
	}
}

// TestCompareWithMkfsHardLinks builds a single-layer tar with a variety of
// hard-link configurations, converts it with both tarconv.Apply (default mode)
// and mkfs.erofs, and asserts the resulting images are identical.
//
// Covered cases:
//   - 2-way hard link (canonical + 1 alias), nlink=2
//   - 3-way hard link (canonical + 2 aliases), nlink=3
//   - Cross-directory hard link (alias in a different dir from canonical)
//   - Hard link to a file with non-root uid/gid (Chown applied to canonical
//     must be reflected on all aliases)
func TestCompareWithMkfsHardLinks(t *testing.T) {
	ts := fixedBuildTime
	tarData := makeTar(t, func(tw *tar.Writer) {
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "a/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "b/", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})

		// 2-way: a/one → a/one-link
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "a/one", Size: 3, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
		tw.Write([]byte("one"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "a/one-link", Linkname: "a/one", Uid: 0, Gid: 0, ModTime: ts})

		// 3-way: a/three, a/three-1, a/three-2 — nlink must be exactly 3
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "a/three", Size: 5, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
		tw.Write([]byte("three"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "a/three-1", Linkname: "a/three", Uid: 0, Gid: 0, ModTime: ts})
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "a/three-2", Linkname: "a/three", Uid: 0, Gid: 0, ModTime: ts})

		// cross-directory: canonical in a/, alias in b/
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "a/shared", Size: 6, Mode: 0o755, Uid: 1000, Gid: 1000, ModTime: ts})
		tw.Write([]byte("shared"))
		tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "b/shared", Linkname: "a/shared", Uid: 1000, Gid: 1000, ModTime: ts})
	})

	goImg := buildGoImage(t, tarData)
	mkfsImg := buildMkfsImage(t, tarData)
	fsckImageBytes(t, "go", goImg)
	fsckImageBytes(t, "mkfs", mkfsImg)
	compareImages(t, goImg, mkfsImg)
}

// TestCompareMergeHardLinksWithMkfs verifies that tarconv.Apply(WithMerge)
// produces the same result as mkfs.erofs operating on the equivalent
// pre-merged tar.
//
// Three sub-cases are tested:
//
//  1. Both canonical and alias in the same layer.
//  2. Canonical in layer 1, alias in layer 2 (cross-layer hard link).
//     The pre-merged tar for mkfs includes both the canonical file and the
//     hard-link entry in one stream.
//  3. Canonical in layer 1, alias in layer 2, with the canonical file
//     updated (overwritten) in layer 2 — alias must reflect the update
//     (nlink=2, new content).
func TestCompareMergeHardLinksWithMkfs(t *testing.T) {
	ts := fixedBuildTime

	t.Run("SameLayer", func(t *testing.T) {
		// Both canonical and alias land in the same layer — identical to the
		// non-merge case, but exercised through WithMerge.
		layer1 := makeTar(t, func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "data", Size: 4, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
			tw.Write([]byte("data"))
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "link", Linkname: "data", Uid: 0, Gid: 0, ModTime: ts})
		})

		// merged image via WithMerge
		out := &buf{}
		w := erofs.Create(out, erofs.WithBuildTime(uint64(ts.Unix()), 0))
		if err := tarconv.Apply(w, bytes.NewReader(layer1), tarconv.WithMerge()); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		mergedImg := out.b

		// equivalent single-layer mkfs image
		mkfsImg := buildMkfsImage(t, layer1)

		fsckImageBytes(t, "merged", mergedImg)
		fsckImageBytes(t, "mkfs", mkfsImg)
		compareImages(t, mergedImg, mkfsImg)
	})

	t.Run("CrossLayer", func(t *testing.T) {
		// Canonical in layer 1, alias in layer 2.
		layer1 := makeTar(t, func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "data", Size: 4, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
			tw.Write([]byte("data"))
		})
		layer2 := makeTar(t, func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "link", Linkname: "data", Uid: 0, Gid: 0, ModTime: ts})
		})

		// merged image via two Apply(WithMerge) calls
		out := &buf{}
		w := erofs.Create(out, erofs.WithBuildTime(uint64(ts.Unix()), 0))
		if err := tarconv.Apply(w, bytes.NewReader(layer1), tarconv.WithMerge()); err != nil {
			t.Fatalf("Apply layer1: %v", err)
		}
		if err := tarconv.Apply(w, bytes.NewReader(layer2), tarconv.WithMerge()); err != nil {
			t.Fatalf("Apply layer2: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		mergedImg := out.b

		// equivalent pre-merged tar for mkfs: canonical + hard link in one stream
		preMerged := makeTar(t, func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "data", Size: 4, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
			tw.Write([]byte("data"))
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "link", Linkname: "data", Uid: 0, Gid: 0, ModTime: ts})
		})
		mkfsImg := buildMkfsImage(t, preMerged)

		fsckImageBytes(t, "merged", mergedImg)
		fsckImageBytes(t, "mkfs", mkfsImg)
		compareImages(t, mergedImg, mkfsImg)
	})

	t.Run("CrossLayerWithUpdate", func(t *testing.T) {
		// Canonical in layer 1, overwritten in layer 2, alias also in layer 2.
		// The final image should have nlink=2 and the new content.
		layer1 := makeTar(t, func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "data", Size: 3, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
			tw.Write([]byte("old"))
		})
		layer2 := makeTar(t, func(tw *tar.Writer) {
			// Overwrite with new content.
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "data", Size: 3, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
			tw.Write([]byte("new"))
			// Hard link to the new version.
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "link", Linkname: "data", Uid: 0, Gid: 0, ModTime: ts})
		})

		out := &buf{}
		w := erofs.Create(out, erofs.WithBuildTime(uint64(ts.Unix()), 0))
		if err := tarconv.Apply(w, bytes.NewReader(layer1), tarconv.WithMerge()); err != nil {
			t.Fatalf("Apply layer1: %v", err)
		}
		if err := tarconv.Apply(w, bytes.NewReader(layer2), tarconv.WithMerge()); err != nil {
			t.Fatalf("Apply layer2: %v", err)
		}
		if err := w.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		mergedImg := out.b

		preMerged := makeTar(t, func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0o755, Uid: 0, Gid: 0, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "data", Size: 3, Mode: 0o644, Uid: 0, Gid: 0, ModTime: ts})
			tw.Write([]byte("new"))
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "link", Linkname: "data", Uid: 0, Gid: 0, ModTime: ts})
		})
		mkfsImg := buildMkfsImage(t, preMerged)

		fsckImageBytes(t, "merged", mergedImg)
		fsckImageBytes(t, "mkfs", mkfsImg)
		compareImages(t, mergedImg, mkfsImg)
	})
}

// TestFsckConvert validates all Convert test outputs against fsck.erofs.
// This runs fsck on every image produced in the main convert_test.go suite.
func TestFsckConvert(t *testing.T) {
	if _, err := exec.LookPath("fsck.erofs"); err != nil {
		t.Skip("fsck.erofs not in PATH")
	}
	ts := fixedBuildTime
	cases := []struct {
		name string
		tar  func(tw *tar.Writer)
	}{
		{"BasicFiles", func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "etc/", Mode: 0o755, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "etc/hosts", Size: 9, Mode: 0o644, ModTime: ts})
			tw.Write([]byte("127.0.0.1"))
		}},
		{"HardLinksOutOfOrder", func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeLink, Name: "early", Linkname: "actual", ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "actual", Size: 4, Mode: 0o644, ModTime: ts})
			tw.Write([]byte("data"))
		}},
		{"DeviceNodes", func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeChar, Name: "dev/null", Mode: 0o666, Devmajor: 1, Devminor: 3, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeFifo, Name: "tmp/pipe", Mode: 0o644, ModTime: ts})
		}},
		{"SetuidSticky", func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "su", Size: 2, Mode: 0o4755, ModTime: ts})
			tw.Write([]byte("su"))
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "tmp/", Mode: 0o1777, ModTime: ts})
		}},
		{"Whiteouts", func(tw *tar.Writer) {
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: "lib/", Mode: 0o755, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh.gone", Size: 0, ModTime: ts})
			tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: "lib/.wh..wh..opq", Size: 0, ModTime: ts})
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tarData := makeTar(t, tc.tar)
			imgData := buildGoImage(t, tarData)
			fsckImageBytes(t, tc.name, imgData)
		})
	}
}

// ----------------------------------------------------------------------------
// Comparison benchmark: walk both images and verify matching stats.
// ----------------------------------------------------------------------------

// BenchmarkImageRoundtrip builds a medium workload, converts it, and reads
// back every entry — measuring end-to-end throughput including image reads.
func BenchmarkImageRoundtrip(b *testing.B) {
	entries := mediumWorkload()
	tarData := buildTarBytes(b, entries)
	b.SetBytes(int64(len(tarData)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out := &buf{}
		w := erofs.Create(out, erofs.WithBuildTime(uint64(fixedBuildTime.Unix()), 0))
		if err := tarconv.Apply(w, bytes.NewReader(tarData)); err != nil {
			b.Fatalf("Convert: %v", err)
		}
		if err := w.Close(); err != nil {
			b.Fatalf("Close: %v", err)
		}
		img, err := erofs.Open(bytes.NewReader(out.b))
		if err != nil {
			b.Fatalf("Open: %v", err)
		}
		rd, _ := img.(readDirer)
		ls, _ := img.(lstater)
		var walkCount int
		var walkDir func(string)
		walkDir = func(dir string) {
			des, _ := rd.ReadDir(dir)
			for _, de := range des {
				var p string
				if dir == "." {
					p = de.Name()
				} else {
					p = dir + "/" + de.Name()
				}
				ls.Lstat(p)
				walkCount++
				if de.IsDir() {
					walkDir(p)
				}
			}
		}
		walkDir(".")
		_ = walkCount
	}
}

// ----------------------------------------------------------------------------
// Helpers used only in this file.
// ----------------------------------------------------------------------------

// mediumSyntheticTar returns tar bytes for the medium workload.
// Reused from bench_test.go workload definitions.
func mediumSyntheticTar(t testing.TB) []byte {
	t.Helper()
	return buildTarBytes(t, mediumWorkload())
}

// pathBase returns the last element of a /-separated path.
func pathBase(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// pathDir returns all but the last element of a /-separated path.
func pathDir(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[:i]
	}
	return "."
}

// writeTarFile writes a tar.Header plus optional data to a temporary file,
// returns the path. Caller must remove.
func writeTarToFile(t testing.TB, tarData []byte) string {
	t.Helper()
	f, err := os.CreateTemp("", "cmp-*.tar")
	if err != nil {
		t.Fatalf("create tar file: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(tarData); err != nil {
		t.Fatalf("write tar file: %v", err)
	}
	return f.Name()
}

// readMkfsImage runs mkfs.erofs on a tar file and returns the image bytes.
func readMkfsImageFromFile(t testing.TB, tarPath, outDir string) []byte {
	t.Helper()
	outPath := filepath.Join(outDir, "out.erofs")
	f, err := os.Open(tarPath)
	if err != nil {
		t.Fatalf("open tar: %v", err)
	}
	defer f.Close()
	args := []string{"--tar=f", "--aufs", "--quiet", "-Enoinline_data",
		"-T" + fixedBuildTimeStr, "--all-time", outPath}
	cmd := exec.CommandContext(context.Background(), "mkfs.erofs", args...)
	cmd.Stdin = f
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mkfs.erofs: %v\n%s", err, out)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read image: %v", err)
	}
	return data
}

// unused – kept to avoid "imported and not used" in pathBase/pathDir.
var _ = pathBase
var _ = pathDir
var _ = writeTarToFile
var _ = readMkfsImageFromFile
var _ = mediumSyntheticTar
