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

package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/containerd/continuity/testutil"
)

// TODO: Additional tests
// - capability test (requires privilege)
// - chown test (requires privilege)
// - symlink test
// - hardlink test

func skipDiffTestOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("diff implementation is incomplete on windows")
	}
}

func skipDiffTestOnNonLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("diff implementation is incomplete on %s", runtime.GOOS)
	}
}

func TestSimpleDiff(t *testing.T) {
	skipDiffTestOnWindows(t)
	l1 := fstest.Apply(
		fstest.CreateDir("/etc", 0o755),
		fstest.CreateFile("/etc/hosts", []byte("mydomain 10.0.0.1"), 0o644),
		fstest.CreateFile("/etc/profile", []byte("PATH=/usr/bin"), 0o644),
		fstest.CreateFile("/etc/unchanged", []byte("PATH=/usr/bin"), 0o644),
		fstest.CreateFile("/etc/unexpected", []byte("#!/bin/sh"), 0o644),
	)
	l2 := fstest.Apply(
		fstest.CreateFile("/etc/hosts", []byte("mydomain 10.0.0.120"), 0o644),
		fstest.CreateFile("/etc/profile", []byte("PATH=/usr/bin"), 0o666),
		fstest.CreateDir("/root", 0o700),
		fstest.CreateFile("/root/.bashrc", []byte("PATH=/usr/sbin:/usr/bin"), 0o644),
		fstest.Remove("/etc/unexpected"),
	)
	diff := []TestChange{
		Modify("/etc/hosts"),
		Modify("/etc/profile"),
		Delete("/etc/unexpected"),
		Add("/root"),
		Add("/root/.bashrc"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestEmptyFileDiff(t *testing.T) {
	skipDiffTestOnWindows(t)
	tt := time.Now().Truncate(time.Second)
	l1 := fstest.Apply(
		fstest.CreateDir("/etc", 0o755),
		fstest.CreateFile("/etc/empty", []byte(""), 0o644),
		fstest.Chtimes("/etc/empty", tt, tt),
	)
	l2 := fstest.Apply()
	diff := []TestChange{}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestNestedDeletion(t *testing.T) {
	skipDiffTestOnWindows(t)
	l1 := fstest.Apply(
		fstest.CreateDir("/d0", 0o755),
		fstest.CreateDir("/d1", 0o755),
		fstest.CreateDir("/d1/d2", 0o755),
		fstest.CreateFile("/d1/d2/f1", []byte("mydomain 10.0.0.1"), 0o644),
	)
	l2 := fstest.Apply(
		fstest.RemoveAll("/d0"),
		fstest.RemoveAll("/d1"),
	)
	diff := []TestChange{
		Delete("/d0"),
		Delete("/d1"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestDirectoryReplace(t *testing.T) {
	skipDiffTestOnWindows(t)
	l1 := fstest.Apply(
		fstest.CreateDir("/dir1", 0o755),
		fstest.CreateFile("/dir1/f1", []byte("#####"), 0o644),
		fstest.CreateDir("/dir1/f2", 0o755),
		fstest.CreateFile("/dir1/f2/f3", []byte("#!/bin/sh"), 0o644),
	)
	l2 := fstest.Apply(
		fstest.CreateFile("/dir1/f11", []byte("#New file here"), 0o644),
		fstest.RemoveAll("/dir1/f2"),
		fstest.CreateFile("/dir1/f2", []byte("Now file"), 0o666),
	)
	diff := []TestChange{
		Add("/dir1/f11"),
		Modify("/dir1/f2"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestRemoveDirectoryTree(t *testing.T) {
	l1 := fstest.Apply(
		fstest.CreateDir("/dir1/dir2/dir3", 0o755),
		fstest.CreateFile("/dir1/f1", []byte("f1"), 0o644),
		fstest.CreateFile("/dir1/dir2/f2", []byte("f2"), 0o644),
	)
	l2 := fstest.Apply(
		fstest.RemoveAll("/dir1"),
	)
	diff := []TestChange{
		Delete("/dir1"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestRemoveDirectoryTreeWithDash(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows fails this test with `-` files reported as modified")
	}
	l1 := fstest.Apply(
		fstest.CreateDir("/dir1/dir2/dir3", 0o755),
		fstest.CreateFile("/dir1/f1", []byte("f1"), 0o644),
		fstest.CreateFile("/dir1/dir2/f2", []byte("f2"), 0o644),
		fstest.CreateDir("/dir1-before", 0o755),
		fstest.CreateFile("/dir1-before/f2", []byte("f2"), 0o644),
	)
	l2 := fstest.Apply(
		fstest.RemoveAll("/dir1"),
	)
	diff := []TestChange{
		Delete("/dir1"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestFileReplace(t *testing.T) {
	l1 := fstest.Apply(
		fstest.CreateFile("/dir1", []byte("a file, not a directory"), 0o644),
	)
	l2 := fstest.Apply(
		fstest.Remove("/dir1"),
		fstest.CreateDir("/dir1/dir2", 0o755),
		fstest.CreateFile("/dir1/dir2/f1", []byte("also a file"), 0o644),
	)
	diff := []TestChange{
		Modify("/dir1"),
		Add("/dir1/dir2"),
		Add("/dir1/dir2/f1"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestDiffDirChangeWithOverlayfs(t *testing.T) {
	skipDiffTestOnNonLinux(t)
	testutil.RequiresRoot(t)

	l1 := fstest.Apply(
		fstest.CreateDir("/dir1", 0700),
		fstest.CreateFile("/dir1/f", []byte("/dir1/f"), 0644),
		fstest.CreateDir("/dir1/d", 0700),
		fstest.CreateFile("/dir1/d/f", []byte("/dir1/d/f"), 0644),

		fstest.CreateDir("/dir2", 0700),
		fstest.CreateDir("/dir2/d", 0700),
		fstest.CreateFile("/dir2/d/f", []byte("/dir2/d/f"), 0644),

		fstest.CreateDir("/dir3", 0700),
		fstest.CreateFile("/dir3/f", []byte("/dir3/f"), 0644),
	)

	l2 := fstest.Apply(
		fstest.CreateDir("/dir1", 0700),
		fstest.CreateFile("/dir1/f", []byte("/dir1/f-diff"), 0644),
		fstest.CreateDeviceFile("/dir1/d", os.ModeDevice|os.ModeCharDevice, 0, 0),

		fstest.CreateDir("/dir2", 0700),
		fstest.CreateDir("/dir2/d", 0700),
		fstest.CreateFile("/dir2/d/f", []byte("/dir2/d/f-diff"), 0644),

		fstest.CreateDir("/dir3", 0700),
		// TODO(fuweid): check kernel version before apply
		fstest.SetXAttr("/dir3", "user.overlay.opaque", "y"),
	)

	diff := []TestChange{
		Modify("/dir1"),
		Modify("/dir1/f"),
		Delete("/dir1/d"),

		Modify("/dir2"),
		Modify("/dir2/d"),
		Modify("/dir2/d/f"),

		Modify("/dir3"),
		Delete("/dir3/.wh..opq"),
	}

	if err := testDiffDirChange(l1, l2, DiffSourceOverlayFS, diff); err != nil {
		t.Fatalf("failed diff dir change: %+v", err)
	}
}

func TestParentDirectoryPermission(t *testing.T) {
	skipDiffTestOnWindows(t)
	l1 := fstest.Apply(
		fstest.CreateDir("/dir1", 0o700),
		fstest.CreateDir("/dir2", 0o751),
		fstest.CreateDir("/dir3", 0o777),
	)
	l2 := fstest.Apply(
		fstest.CreateDir("/dir1/d", 0o700),
		fstest.CreateFile("/dir1/d/f", []byte("irrelevant"), 0o644),
		fstest.CreateFile("/dir1/f", []byte("irrelevant"), 0o644),
		fstest.CreateFile("/dir2/f", []byte("irrelevant"), 0o644),
		fstest.CreateFile("/dir3/f", []byte("irrelevant"), 0o644),
	)
	diff := []TestChange{
		Add("/dir1/d"),
		Add("/dir1/d/f"),
		Add("/dir1/f"),
		Add("/dir2/f"),
		Add("/dir3/f"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

func TestUpdateWithSameTime(t *testing.T) {
	skipDiffTestOnWindows(t)
	tt := time.Now().Truncate(time.Second)
	t1 := tt.Add(5 * time.Nanosecond)
	t2 := tt.Add(6 * time.Nanosecond)
	l1 := fstest.Apply(
		fstest.CreateFile("/file-modified-time", []byte("1"), 0o644),
		fstest.Chtimes("/file-modified-time", t1, t1),
		fstest.CreateFile("/file-no-change", []byte("1"), 0o644),
		fstest.Chtimes("/file-no-change", t1, t1),
		fstest.CreateFile("/file-same-time", []byte("1"), 0o644),
		fstest.Chtimes("/file-same-time", t1, t1),
		fstest.CreateFile("/file-truncated-time-1", []byte("1"), 0o644),
		fstest.Chtimes("/file-truncated-time-1", tt, tt),
		fstest.CreateFile("/file-truncated-time-2", []byte("1"), 0o644),
		fstest.Chtimes("/file-truncated-time-2", tt, tt),
		fstest.CreateFile("/file-truncated-time-3", []byte("1"), 0o644),
		fstest.Chtimes("/file-truncated-time-3", t1, t1),
	)
	l2 := fstest.Apply(
		fstest.CreateFile("/file-modified-time", []byte("2"), 0o644),
		fstest.Chtimes("/file-modified-time", t2, t2),
		fstest.CreateFile("/file-no-change", []byte("1"), 0o644),
		fstest.Chtimes("/file-no-change", t1, t1),
		fstest.CreateFile("/file-same-time", []byte("2"), 0o644),
		fstest.Chtimes("/file-same-time", t1, t1),
		fstest.CreateFile("/file-truncated-time-1", []byte("1"), 0o644),
		fstest.Chtimes("/file-truncated-time-1", t1, t1),
		fstest.CreateFile("/file-truncated-time-2", []byte("2"), 0o644),
		fstest.Chtimes("/file-truncated-time-2", tt, tt),
		fstest.CreateFile("/file-truncated-time-3", []byte("1"), 0o644),
		fstest.Chtimes("/file-truncated-time-3", tt, tt),
	)
	diff := []TestChange{
		Modify("/file-modified-time"),
		// Include changes with truncated timestamps. Comparing newly
		// extracted tars which have truncated timestamps will be
		// expected to produce changes. The expectation is that diff
		// archives are generated once and kept, newly generated diffs
		// will not consider cases where only one side is truncated.
		Modify("/file-truncated-time-1"),
		Modify("/file-truncated-time-2"),
		Modify("/file-truncated-time-3"),
	}

	if err := testDiffWithBase(t, l1, l2, diff); err != nil {
		t.Fatalf("Failed diff with base: %+v", err)
	}
}

// buildkit#172
func TestLchtimes(t *testing.T) {
	skipDiffTestOnWindows(t)
	mtimes := []time.Time{
		time.Unix(0, 0),  // nsec is 0
		time.Unix(0, 42), // nsec > 0
	}
	for _, mtime := range mtimes {
		atime := time.Unix(424242, 42)
		l1 := fstest.Apply(
			fstest.CreateFile("/foo", []byte("foo"), 0o644),
			fstest.Symlink("/foo", "/lnk0"),
			fstest.Lchtimes("/lnk0", atime, mtime),
		)
		l2 := fstest.Apply() // empty
		diff := []TestChange{}
		if err := testDiffWithBase(t, l1, l2, diff); err != nil {
			t.Fatalf("Failed diff with base: %+v", err)
		}
	}
}

func testDiffWithBase(t testing.TB, base, diff fstest.Applier, expected []TestChange) error {
	t1 := t.TempDir()
	t2 := t.TempDir()

	if err := base.Apply(t1); err != nil {
		return fmt.Errorf("failed to apply base filesystem: %w", err)
	}

	if err := CopyDir(t2, t1); err != nil {
		return fmt.Errorf("failed to copy base directory: %w", err)
	}

	if err := diff.Apply(t2); err != nil {
		return fmt.Errorf("failed to apply diff filesystem: %w", err)
	}

	changes, err := collectChanges(t1, t2)
	if err != nil {
		return fmt.Errorf("failed to collect changes: %w", err)
	}

	return checkChanges(t2, changes, expected)
}

func TestBaseDirectoryChanges(t *testing.T) {
	apply := fstest.Apply(
		fstest.CreateDir("/etc", 0o755),
		fstest.CreateFile("/etc/hosts", []byte("mydomain 10.0.0.1"), 0o644),
		fstest.CreateFile("/etc/profile", []byte("PATH=/usr/bin"), 0o644),
		fstest.CreateDir("/root", 0o700),
		fstest.CreateFile("/root/.bashrc", []byte("PATH=/usr/sbin:/usr/bin"), 0o644),
	)
	changes := []TestChange{
		Add("/etc"),
		Add("/etc/hosts"),
		Add("/etc/profile"),
		Add("/root"),
		Add("/root/.bashrc"),
	}

	if err := testDiffWithoutBase(t, apply, changes); err != nil {
		t.Fatalf("Failed diff without base: %+v", err)
	}
}

func testDiffWithoutBase(t testing.TB, apply fstest.Applier, expected []TestChange) error {
	tmp := t.TempDir()
	if err := apply.Apply(tmp); err != nil {
		return fmt.Errorf("failed to apply filesytem changes: %w", err)
	}

	changes, err := collectChanges("", tmp)
	if err != nil {
		return fmt.Errorf("failed to collect changes: %w", err)
	}

	return checkChanges(tmp, changes, expected)
}

func testDiffDirChange(base, diff fstest.Applier, source DiffSource, expected []TestChange) error {
	baseTmp, err := os.MkdirTemp("", "fast-diff-base-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(baseTmp)

	diffTmp, err := os.MkdirTemp("", "fast-diff-diff-")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(diffTmp)

	if err := base.Apply(baseTmp); err != nil {
		return fmt.Errorf("failed to apply filesytem changes: %w", err)
	}

	if err := diff.Apply(diffTmp); err != nil {
		return fmt.Errorf("failed to apply filesytem changes: %w", err)
	}

	changes, err := collectDiffDirChanges(baseTmp, diffTmp, source)
	if err != nil {
		return fmt.Errorf("failed to collect diff dir changes: %w", err)
	}
	return checkChanges(diffTmp, changes, expected)
}

func checkChanges(root string, changes, expected []TestChange) error {
	sort.SliceStable(changes, func(i, j int) bool {
		return changes[i].Path < changes[j].Path
	})

	sort.SliceStable(expected, func(i, j int) bool {
		return expected[i].Path < expected[j].Path
	})

	if len(changes) != len(expected) {
		return fmt.Errorf("Unexpected number of changes:\n%s", diffString(changes, expected))
	}
	for i := range changes {
		if changes[i].Path != expected[i].Path || changes[i].Kind != expected[i].Kind {
			return fmt.Errorf("Unexpected change at %d:\n%s", i, diffString(changes, expected))
		}
		if changes[i].Kind != ChangeKindDelete {
			filename := filepath.Join(root, changes[i].Path)
			efi, err := os.Stat(filename)
			if err != nil {
				return fmt.Errorf("failed to stat %q: %w", filename, err)
			}
			afi := changes[i].FileInfo
			if afi.Size() != efi.Size() {
				return fmt.Errorf("Unexpected change size %d, %q has size %d", afi.Size(), filename, efi.Size())
			}
			if afi.Mode() != efi.Mode() {
				return fmt.Errorf("Unexpected change mode %s, %q has mode %s", afi.Mode(), filename, efi.Mode())
			}
			if afi.ModTime() != efi.ModTime() {
				return fmt.Errorf("Unexpected change modtime %s, %q has modtime %s", afi.ModTime(), filename, efi.ModTime())
			}
			if expected := filepath.Join(root, changes[i].Path); changes[i].Source != expected {
				return fmt.Errorf("Unexpected source path %s, expected %s", changes[i].Source, expected)
			}
		}
	}

	return nil
}

type TestChange struct {
	Kind     ChangeKind
	Path     string
	FileInfo os.FileInfo
	Source   string
}

func collectChanges(a, b string) ([]TestChange, error) {
	changes := []TestChange{}
	err := Changes(context.Background(), a, b, func(k ChangeKind, p string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		changes = append(changes, TestChange{
			Kind:     k,
			Path:     p,
			FileInfo: f,
			Source:   filepath.Join(b, p),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute changes: %w", err)
	}

	return changes, nil
}

func collectDiffDirChanges(baseDir, diffDir string, source DiffSource) ([]TestChange, error) {
	changes := []TestChange{}
	err := DiffDirChanges(context.Background(), baseDir, diffDir, source, func(k ChangeKind, p string, f os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		changes = append(changes, TestChange{
			Kind:     k,
			Path:     p,
			FileInfo: f,
			Source:   filepath.Join(diffDir, p),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to compute changes: %w", err)
	}
	return changes, nil
}

func diffString(c1, c2 []TestChange) string {
	return fmt.Sprintf("got(%d):\n%s\nexpected(%d):\n%s", len(c1), changesString(c1), len(c2), changesString(c2))
}

func changesString(c []TestChange) string {
	strs := make([]string, len(c))
	for i := range c {
		strs[i] = fmt.Sprintf("\t%s\t%s", c[i].Kind, c[i].Path)
	}
	return strings.Join(strs, "\n")
}

func Add(p string) TestChange {
	return TestChange{
		Kind: ChangeKindAdd,
		Path: filepath.FromSlash(p),
	}
}

func Delete(p string) TestChange {
	return TestChange{
		Kind: ChangeKindDelete,
		Path: filepath.FromSlash(p),
	}
}

func Modify(p string) TestChange {
	return TestChange{
		Kind: ChangeKindModify,
		Path: filepath.FromSlash(p),
	}
}
