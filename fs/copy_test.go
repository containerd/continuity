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
	_ "crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/containerd/continuity/fs/fstest"
)

// TODO: Create copy directory which requires privilege
//  chown
//  mknod
//  setxattr fstest.SetXAttr("/home", "trusted.overlay.opaque", "y"),

func TestCopyDirectory(t *testing.T) {
	apply := fstest.Apply(
		fstest.CreateDir("/etc/", 0o755),
		fstest.CreateFile("/etc/hosts", []byte("localhost 127.0.0.1"), 0o644),
		fstest.Link("/etc/hosts", "/etc/hosts.allow"),
		fstest.CreateDir("/usr/local/lib", 0o755),
		fstest.CreateFile("/usr/local/lib/libnothing.so", []byte{0x00, 0x00}, 0o755),
		fstest.Symlink("libnothing.so", "/usr/local/lib/libnothing.so.2"),
		fstest.CreateDir("/home", 0o755),
	)

	if err := testCopy(t, apply); err != nil {
		t.Fatalf("Copy test failed: %+v", err)
	}
}

// This test used to fail because link-no-nothing.txt would be copied first,
// then file operations in dst during the CopyDir would follow the symlink and
// fail.
func TestCopyDirectoryWithLocalSymlink(t *testing.T) {
	apply := fstest.Apply(
		fstest.CreateFile("nothing.txt", []byte{0x00, 0x00}, 0o755),
		fstest.Symlink("nothing.txt", "link-no-nothing.txt"),
	)

	if err := testCopy(t, apply); err != nil {
		t.Fatalf("Copy test failed: %+v", err)
	}
}

// TestCopyWithLargeFile tests copying a file whose size > 2^32 bytes.
func TestCopyWithLargeFile(t *testing.T) {
	if testing.Short() {
		t.SkipNow()
	}
	apply := fstest.Apply(
		fstest.CreateDir("/banana", 0o755),
		fstest.CreateRandomFile("/banana/split", time.Now().UnixNano(), 3*1024*1024*1024, 0o644),
	)

	if err := testCopy(t, apply); err != nil {
		t.Fatal(err)
	}
}

func testCopy(t testing.TB, apply fstest.Applier) error {
	t1 := t.TempDir()
	t2 := t.TempDir()

	if err := apply.Apply(t1); err != nil {
		return fmt.Errorf("failed to apply changes: %w", err)
	}

	if err := CopyDir(t2, t1); err != nil {
		return fmt.Errorf("failed to copy: %w", err)
	}

	return fstest.CheckDirectoryEqual(t1, t2)
}

func BenchmarkLargeCopy100MB(b *testing.B) {
	benchmarkLargeCopyFile(b, 100*1024*1024)
}

func BenchmarkLargeCopy1GB(b *testing.B) {
	benchmarkLargeCopyFile(b, 1024*1024*1024)
}

func benchmarkLargeCopyFile(b *testing.B, size int64) {
	b.StopTimer()
	base := b.TempDir()
	apply := fstest.Apply(
		fstest.CreateRandomFile("/large", time.Now().UnixNano(), size, 0o644),
	)
	if err := apply.Apply(base); err != nil {
		b.Fatal("failed to apply changes:", err)
	}

	for i := 0; i < b.N; i++ {
		copied := b.TempDir()
		b.StartTimer()
		if err := CopyDir(copied, base); err != nil {
			b.Fatal("failed to copy:", err)
		}
		b.StopTimer()
		if i == 0 {
			if err := fstest.CheckDirectoryEqual(base, copied); err != nil {
				b.Fatal("check failed:", err)
			}
		}
		os.RemoveAll(copied)
	}
}

func TestCopyFileWithSync(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src"
	dst := dir + "/dst"

	data := []byte("this is a file sync test")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(dst, src, WithFileSync()); err != nil {
		t.Fatalf("CopyFile with WithFileSync failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("content mismatch: got %q, want %q", got, data)
	}
}

func TestCopyFileWithoutSync(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src"
	dst := dir + "/dst"

	data := []byte("this is a no-sync test")
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(dst, src); err != nil {
		t.Fatalf("CopyFile without sync failed: %v", err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("content mismatch: got %q, want %q", got, data)
	}
}

func TestCopyDirWithFileSync(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	apply := fstest.Apply(
		fstest.CreateDir("/subdir", 0o755),
		fstest.CreateFile("/subdir/file.txt", []byte("this is a file sync test"), 0o644),
		fstest.CreateFile("/root.txt", []byte("root file"), 0o644),
	)
	if err := apply.Apply(src); err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(dst, src, WithCopyFileSync()); err != nil {
		t.Fatalf("CopyDir with WithCopyFileSync failed: %v", err)
	}

	if err := fstest.CheckDirectoryEqual(src, dst); err != nil {
		t.Fatal(err)
	}
}

func TestCopyDirWithDirSync(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	apply := fstest.Apply(
		fstest.CreateDir("/subdir", 0o755),
		fstest.CreateFile("/subdir/file.txt", []byte("this is a dir sync test"), 0o644),
		fstest.CreateFile("/root.txt", []byte("root file"), 0o644),
	)
	if err := apply.Apply(src); err != nil {
		t.Fatal(err)
	}

	if err := CopyDir(dst, src, WithCopyDirSync()); err != nil {
		t.Fatalf("CopyDir with WithCopyDirSync failed: %v", err)
	}

	if err := fstest.CheckDirectoryEqual(src, dst); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkCopyFile(b *testing.B) {
	benchmarkCopyFile(b, false)
}

func BenchmarkCopyFileWithSync(b *testing.B) {
	benchmarkCopyFile(b, true)
}

func benchmarkCopyFile(b *testing.B, sync bool) {
	b.StopTimer()
	base := b.TempDir()
	apply := fstest.Apply(
		fstest.CreateRandomFile("/benchfile", time.Now().UnixNano(), 10*1024*1024, 0o644),
	)
	if err := apply.Apply(base); err != nil {
		b.Fatal("failed to apply changes:", err)
	}
	src := base + "/benchfile"

	var opts []CopyFileOpt
	if sync {
		opts = append(opts, WithFileSync())
	}

	for i := 0; i < b.N; i++ {
		dst := b.TempDir() + "/dst"
		b.StartTimer()
		if err := CopyFile(dst, src, opts...); err != nil {
			b.Fatal("failed to copy:", err)
		}
		b.StopTimer()
	}
}

func BenchmarkCopyDir(b *testing.B) {
	benchmarkCopyDir(b, false)
}

func BenchmarkCopyDirWithSync(b *testing.B) {
	benchmarkCopyDir(b, true)
}

func benchmarkCopyDir(b *testing.B, sync bool) {
	b.StopTimer()
	base := b.TempDir()

	appliers := []fstest.Applier{
		fstest.CreateDir("/manyfiles", 0o755),
	}
	for i := 0; i < 500; i++ {
		appliers = append(appliers,
			fstest.CreateRandomFile(fmt.Sprintf("/manyfiles/file_%04d", i), int64(i), 4096, 0o644),
		)
	}
	if err := fstest.Apply(appliers...).Apply(base); err != nil {
		b.Fatal("failed to apply:", err)
	}

	var opts []CopyDirOpt
	if sync {
		opts = append(opts, WithCopyDirSync())
	}

	for i := 0; i < b.N; i++ {
		dst := b.TempDir()
		b.StartTimer()
		if err := CopyDir(dst, base+"/manyfiles", opts...); err != nil {
			b.Fatal("failed to copy:", err)
		}
		b.StopTimer()
	}
}
