//go:build linux

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
	"bytes"
	"errors"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/containerd/continuity/testutil"
	"github.com/containerd/continuity/testutil/loopback"
)

func TestCopyFileSparse(t *testing.T) {
	dir := t.TempDir()

	type testCase struct {
		name string
		// parts alternates: data length, hole length, data length, ...
		// A 0 data length at the start means the file begins with a hole.
		parts []int64
	}

	tests := []testCase{
		{
			name:  "DataHoleData",
			parts: []int64{4096, 1024 * 1024, 4096},
		},
		{
			name:  "HoleOnly",
			parts: []int64{0, 1024 * 1024, 1},
		},
		{
			name:  "HoleAtStart",
			parts: []int64{0, 1024 * 1024, 4096},
		},
		{
			name:  "HoleAtEnd",
			parts: []int64{4096, 1024 * 1024},
		},
		{
			name:  "MultipleHoles",
			parts: []int64{4096, 512 * 1024, 4096, 512 * 1024, 4096},
		},
		{
			name:  "NoHoles",
			parts: []int64{64 * 1024},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srcPath := filepath.Join(dir, tc.name+"-src")
			dstPath := filepath.Join(dir, tc.name+"-dst")

			applier := createSparseFile(tc.name+"-src", 42, 0o644, tc.parts...)
			if err := applier.Apply(dir); err != nil {
				t.Fatal(err)
			}

			if err := CopyFile(dstPath, srcPath); err != nil {
				t.Fatalf("CopyFile failed: %v", err)
			}

			// Verify content matches exactly.
			srcData, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatal(err)
			}
			dstData, err := os.ReadFile(dstPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(srcData, dstData) {
				t.Fatal("source and destination file contents differ")
			}

			// Verify sparseness is preserved: destination should not use
			// significantly more blocks than the source.
			srcStat, err := os.Stat(srcPath)
			if err != nil {
				t.Fatal(err)
			}
			dstStat, err := os.Stat(dstPath)
			if err != nil {
				t.Fatal(err)
			}

			srcBlocks := srcStat.Sys().(*syscall.Stat_t).Blocks
			dstBlocks := dstStat.Sys().(*syscall.Stat_t).Blocks

			t.Logf("src size=%d blocks=%d, dst size=%d blocks=%d",
				srcStat.Size(), srcBlocks, dstStat.Size(), dstBlocks)

			if srcStat.Size() != dstStat.Size() {
				t.Fatalf("size mismatch: src=%d dst=%d", srcStat.Size(), dstStat.Size())
			}

			// Allow some slack for filesystem metadata, but destination
			// should not use more than 10% extra blocks.
			maxBlocks := srcBlocks + srcBlocks/10 + 8
			if dstBlocks > maxBlocks {
				t.Fatalf("destination is not sparse: src blocks=%d, dst blocks=%d (max allowed=%d)",
					srcBlocks, dstBlocks, maxBlocks)
			}
		})
	}
}

func TestCopyFileSparseWithSync(t *testing.T) {
	dir := t.TempDir()

	type testCase struct {
		name  string
		parts []int64
	}

	tests := []testCase{
		{
			name:  "DataHoleData",
			parts: []int64{4096, 1024 * 1024, 4096},
		},
		{
			name:  "NoHoles",
			parts: []int64{64 * 1024},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srcPath := filepath.Join(dir, tc.name+"-sync-src")
			dstPath := filepath.Join(dir, tc.name+"-sync-dst")

			applier := createSparseFile(tc.name+"-sync-src", 42, 0o644, tc.parts...)
			if err := applier.Apply(dir); err != nil {
				t.Fatal(err)
			}

			if err := CopyFile(dstPath, srcPath, WithFileSync()); err != nil {
				t.Fatalf("CopyFile with WithFileSync failed: %v", err)
			}

			// Verify content matches exactly.
			srcData, err := os.ReadFile(srcPath)
			if err != nil {
				t.Fatal(err)
			}
			dstData, err := os.ReadFile(dstPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(srcData, dstData) {
				t.Fatal("source and destination file contents differ")
			}

			// Verify sparseness is preserved.
			srcStat, err := os.Stat(srcPath)
			if err != nil {
				t.Fatal(err)
			}
			dstStat, err := os.Stat(dstPath)
			if err != nil {
				t.Fatal(err)
			}

			srcBlocks := srcStat.Sys().(*syscall.Stat_t).Blocks
			dstBlocks := dstStat.Sys().(*syscall.Stat_t).Blocks

			t.Logf("src size=%d blocks=%d, dst size=%d blocks=%d",
				srcStat.Size(), srcBlocks, dstStat.Size(), dstBlocks)

			if srcStat.Size() != dstStat.Size() {
				t.Fatalf("size mismatch: src=%d dst=%d", srcStat.Size(), dstStat.Size())
			}

			maxBlocks := srcBlocks + srcBlocks/10 + 8
			if dstBlocks > maxBlocks {
				t.Fatalf("destination is not sparse: src blocks=%d, dst blocks=%d (max allowed=%d)",
					srcBlocks, dstBlocks, maxBlocks)
			}
		})
	}
}

// TestCopyFileRangeErrors checks how copyFile reacts to errors from
// copy_file_range(2). Filesystems that do not implement the call (for
// example eCryptfs, which returns EINVAL) must fall back to a plain copy,
// while unrelated errors such as EIO must still be reported.
func TestCopyFileRangeErrors(t *testing.T) {
	orig := copyFileRange
	t.Cleanup(func() { copyFileRange = orig })

	files := []struct {
		name  string
		parts []int64
	}{
		{name: "Sparse", parts: []int64{4096, 1024 * 1024, 4096}},
		{name: "NoHoles", parts: []int64{64 * 1024}},
	}

	tests := []struct {
		name     string
		errno    syscall.Errno
		fallback bool
	}{
		{name: "EINVAL", errno: syscall.EINVAL, fallback: true},
		{name: "EXDEV", errno: syscall.EXDEV, fallback: true},
		{name: "ENOSYS", errno: syscall.ENOSYS, fallback: true},
		{name: "EOPNOTSUPP", errno: syscall.EOPNOTSUPP, fallback: true},
		{name: "EIO", errno: syscall.EIO, fallback: false},
	}

	for _, tc := range tests {
		for _, f := range files {
			t.Run(tc.name+"/"+f.name, func(t *testing.T) {
				dir := t.TempDir()
				srcName := "src"
				srcPath := filepath.Join(dir, srcName)
				dstPath := filepath.Join(dir, "dst")

				if err := createSparseFile(srcName, 42, 0o644, f.parts...).Apply(dir); err != nil {
					t.Fatal(err)
				}

				calls := 0
				copyFileRange = func(int, *int64, int, *int64, int, int) (int, error) {
					calls++
					return 0, tc.errno
				}

				err := CopyFile(dstPath, srcPath)
				if calls == 0 {
					t.Skip("copy_file_range was not reached (no SEEK_DATA support on this filesystem?)")
				}

				if !tc.fallback {
					if err == nil {
						t.Fatalf("expected an error for %v, got nil", tc.errno)
					}
					if !errors.Is(err, tc.errno) {
						t.Fatalf("expected error wrapping %v, got %v", tc.errno, err)
					}
					if !strings.Contains(err.Error(), "copy_file_range failed") {
						t.Fatalf("expected a copy_file_range failure, got %v", err)
					}
					return
				}

				if err != nil {
					t.Fatalf("expected fallback to plain copy for %v, got error: %v", tc.errno, err)
				}
				srcData, err := os.ReadFile(srcPath)
				if err != nil {
					t.Fatal(err)
				}
				dstData, err := os.ReadFile(dstPath)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(srcData, dstData) {
					t.Fatal("source and destination file contents differ")
				}
			})
		}
	}
}

func TestCopyReflinkWithXFS(t *testing.T) {
	testutil.RequiresRoot(t)
	mnt := t.TempDir()

	loop, err := loopback.New(1 << 30) // sparse file (max=1GB)
	if err != nil {
		t.Fatal(err)
	}
	mkfs := []string{"mkfs.xfs", "-m", "crc=1", "-n", "ftype=1", "-m", "reflink=1"}
	if out, err := exec.Command(mkfs[0], append(mkfs[1:], loop.Device)...).CombinedOutput(); err != nil {
		// not fatal
		t.Skipf("could not mkfs (%v) %s: %v (out: %q)", mkfs, loop.Device, err, string(out))
	}
	loopbackSize, err := loop.HardSize()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Loopback file size (after mkfs (%v)): %d", mkfs, loopbackSize)
	if out, err := exec.Command("mount", loop.Device, mnt).CombinedOutput(); err != nil {
		// not fatal
		t.Skipf("could not mount %s: %v (out: %q)", loop.Device, err, string(out))
	}
	unmounted := false
	defer func() {
		if !unmounted {
			testutil.Unmount(t, mnt)
		}
		loop.Close()
	}()

	aPath := filepath.Join(mnt, "a")
	aSize := int64(100 << 20) // 100MB
	a, err := os.Create(aPath)
	if err != nil {
		t.Fatal(err)
	}
	randReader := rand.New(rand.NewSource(42))
	if _, err := io.CopyN(a, randReader, aSize); err != nil {
		a.Close()
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	bPath := filepath.Join(mnt, "b")
	if err := CopyFile(bPath, aPath); err != nil {
		t.Fatal(err)
	}
	testutil.Unmount(t, mnt)
	unmounted = true
	loopbackSize, err = loop.HardSize()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Loopback file size (after copying a %d-byte file): %d", aSize, loopbackSize)
	// 170 MiB is needed since Ubuntu 24.04. 120 MiB was enough for Ubuntu 22.04.
	allowedSize := int64(170 << 20)
	if loopbackSize > allowedSize {
		t.Fatalf("expected <= %d, got %d", allowedSize, loopbackSize)
	}
}
