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

package fstest

import (
	"os"
	"runtime"
	"testing"
)

func TestCheckDirectoryEqualBasic(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	a := Apply(
		CreateDir("/d", 0o755),
		CreateFile("/d/f1", []byte("hello"), 0o644),
		CreateFile("/f2", []byte("world"), 0o600),
	)
	if err := a.Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err != nil {
		t.Fatalf("identical directories should be equal: %v", err)
	}
}

func TestCheckDirectoryEqualDetectsDifference(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	a1 := Apply(
		CreateFile("/f", []byte("aaa"), 0o644),
	)
	a2 := Apply(
		CreateFile("/f", []byte("bbb"), 0o644),
	)
	if err := a1.Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := a2.Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err == nil {
		t.Fatal("directories with different content should not be equal")
	}
}

func TestCheckDirectoryEqualExtraFile(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	a := Apply(
		CreateFile("/f1", []byte("hello"), 0o644),
	)
	if err := a.Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(d2); err != nil {
		t.Fatal(err)
	}
	// Extra file in d2
	if err := CreateFile("/f2", []byte("extra"), 0o644).Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err == nil {
		t.Fatal("directory with extra file should not be equal")
	}
}

func TestCheckDirectoryEqualMissingFile(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	a1 := Apply(
		CreateFile("/f1", []byte("hello"), 0o644),
		CreateFile("/f2", []byte("world"), 0o644),
	)
	a2 := Apply(
		CreateFile("/f1", []byte("hello"), 0o644),
	)
	if err := a1.Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := a2.Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err == nil {
		t.Fatal("directory with missing file should not be equal")
	}
}

func TestCheckDirectoryEqualSymlinks(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	a := Apply(
		CreateFile("/target", []byte("data"), 0o644),
		Symlink("target", "/link"),
	)
	if err := a.Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err != nil {
		t.Fatalf("identical symlink directories should be equal: %v", err)
	}
}

func TestCheckDirectoryEqualSymlinkDifference(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	a1 := Apply(
		CreateFile("/target1", []byte("data"), 0o644),
		CreateFile("/target2", []byte("data"), 0o644),
		Symlink("target1", "/link"),
	)
	a2 := Apply(
		CreateFile("/target1", []byte("data"), 0o644),
		CreateFile("/target2", []byte("data"), 0o644),
		Symlink("target2", "/link"),
	)
	if err := a1.Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := a2.Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err == nil {
		t.Fatal("directories with different symlink targets should not be equal")
	}
}

func TestCheckDirectoryEqualHardlinks(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	a := Apply(
		CreateFile("/f1", []byte("hello"), 0o644),
		Link("/f1", "/f2"),
	)
	if err := a.Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := a.Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err != nil {
		t.Fatalf("identical hardlink directories should be equal: %v", err)
	}
}

func TestCheckDirectoryEqualPermissionDifference(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support Unix-style file permissions")
	}

	d1 := t.TempDir()
	d2 := t.TempDir()

	if err := CreateFile("/f", []byte("hello"), 0o644).Apply(d1); err != nil {
		t.Fatal(err)
	}
	if err := CreateFile("/f", []byte("hello"), 0o600).Apply(d2); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqual(d1, d2); err == nil {
		t.Fatal("directories with different permissions should not be equal")
	}
}

func TestCheckDirectoryEqualWithApplier(t *testing.T) {
	d := t.TempDir()

	a := Apply(
		CreateDir("/d", 0o755),
		CreateFile("/d/f", []byte("content"), 0o644),
	)
	if err := a.Apply(d); err != nil {
		t.Fatal(err)
	}

	if err := CheckDirectoryEqualWithApplier(d, a); err != nil {
		t.Fatalf("directory should equal its applier: %v", err)
	}
}

func TestBuildResources(t *testing.T) {
	d := t.TempDir()

	a := Apply(
		CreateDir("/a", 0o755),
		CreateFile("/a/f1", []byte("one"), 0o644),
		CreateFile("/b", []byte("two"), 0o600),
		Symlink("b", "/c"),
	)
	if err := a.Apply(d); err != nil {
		t.Fatal(err)
	}

	resources, err := buildResources(d)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 4 entries: /a, /a/f1, /b, /c
	if len(resources) != 4 {
		t.Fatalf("expected 4 resources, got %d", len(resources))
	}

	// Verify sorted order
	for i := 1; i < len(resources); i++ {
		if resources[i].path <= resources[i-1].path {
			t.Fatalf("resources not sorted: %q <= %q", resources[i].path, resources[i-1].path)
		}
	}

	// Verify types
	for _, r := range resources {
		switch r.path {
		case "/a":
			if !r.mode.IsDir() {
				t.Errorf("/a should be directory, got %v", r.mode)
			}
		case "/a/f1":
			if !r.mode.IsRegular() {
				t.Errorf("/a/f1 should be regular file, got %v", r.mode)
			}
			if r.size != 3 {
				t.Errorf("/a/f1 should have size 3, got %d", r.size)
			}
		case "/b":
			if !r.mode.IsRegular() {
				t.Errorf("/b should be regular file, got %v", r.mode)
			}
		case "/c":
			if r.mode&os.ModeSymlink == 0 {
				t.Errorf("/c should be symlink, got %v", r.mode)
			}
			if r.target != "b" {
				t.Errorf("/c target should be 'b', got %q", r.target)
			}
		}
	}
}
