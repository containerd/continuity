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
	"io/fs"
	"testing"

	erofs "github.com/erofs/go-erofs"
)

// layerTar writes the given entries, with contents for regular files.
func layerTar(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	return makeTar(t, func(tw *tar.Writer) {
		for _, h := range entries {
			if h.Typeflag == tar.TypeReg {
				h.Size = int64(len(h.Name))
			}
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if h.Typeflag == tar.TypeReg {
				if _, err := tw.Write([]byte(h.Name)); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}

func whiteout(name string) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644}
}

func dirEntry(name string) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeDir, Name: name, Mode: 0o755}
}

func fileEntry(name string) *tar.Header {
	return &tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644}
}

// checkOpaque reports whether name is a directory marked opaque.
func checkOpaque(t *testing.T, fsys fs.FS, name string, want bool) {
	t.Helper()
	fi := checkStat(t, fsys, name)
	if !fi.IsDir() {
		t.Fatalf("%s must be a directory, got mode %v", name, fi.Mode())
	}
	st, ok := fi.Sys().(*erofs.Stat)
	if !ok {
		t.Fatalf("%s: no erofs.Stat", name)
	}
	if got := st.Xattrs["trusted.overlay.opaque"] == "y"; got != want {
		t.Errorf("%s opaque = %v, want %v (xattrs %v)", name, got, want, st.Xattrs)
	}
}

// TestConvertRecreatedDirOrders verifies that a directory recreated after its
// whiteout becomes opaque and keeps its new content, whichever order the
// directory header and its children appear in.
func TestConvertRecreatedDirOrders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []*tar.Header
		opaque  []string
		files   []string
	}{
		{
			name:    "header before child",
			entries: []*tar.Header{whiteout(".wh.dir"), dirEntry("dir/"), fileEntry("dir/new")},
			opaque:  []string{"dir"},
			files:   []string{"dir/new"},
		},
		{
			name:    "child before header",
			entries: []*tar.Header{whiteout(".wh.dir"), fileEntry("dir/new"), dirEntry("dir/")},
			opaque:  []string{"dir"},
			files:   []string{"dir/new"},
		},
		{
			name:    "no header",
			entries: []*tar.Header{whiteout(".wh.dir"), fileEntry("dir/sub/new")},
			opaque:  []string{"dir"},
			files:   []string{"dir/sub/new"},
		},
		{
			name: "whiteout under a later whiteout",
			entries: []*tar.Header{
				dirEntry("a/"), whiteout("a/.wh.b"), whiteout(".wh.a"), fileEntry("a/b/c"),
			},
			opaque: []string{"a"},
			files:  []string{"a/b/c"},
		},
		{
			name:    "opaque marker",
			entries: []*tar.Header{whiteout(".wh.dir"), whiteout("dir/.wh..wh..opq"), fileEntry("dir/new")},
			opaque:  []string{"dir"},
			files:   []string{"dir/new"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := openImage(t, buildImage(t, layerTar(t, tc.entries...)))
			for _, d := range tc.opaque {
				checkOpaque(t, fsys, d, true)
			}
			for _, f := range tc.files {
				checkFile(t, fsys, f, f)
			}
		})
	}
}

// TestConvertWhiteoutReplacedByFile verifies that a file recreated at a
// whited-out path replaces the whiteout and is not turned into a directory.
func TestConvertWhiteoutReplacedByFile(t *testing.T) {
	fsys := openImage(t, buildImage(t, layerTar(t, whiteout(".wh.f"), fileEntry("f"))))
	checkFile(t, fsys, "f", "f")
}

// TestConvertWhiteoutReplacedByFileThenDir verifies that a directory added
// after a file replaced a whiteout is still opaque: the layer deleted the
// lower entry before recreating it.
func TestConvertWhiteoutReplacedByFileThenDir(t *testing.T) {
	fsys := openImage(t, buildImage(t, layerTar(t, whiteout(".wh.f"), fileEntry("f"), dirEntry("f/"), fileEntry("f/new"))))
	checkOpaque(t, fsys, "f", true)
	checkFile(t, fsys, "f/new", "f/new")
}

// TestMergeDirOverDevice verifies that in merge mode a directory replacing a
// lower character device 0:0 is a plain directory: merge mode records no
// whiteouts and adds no overlay metadata.
func TestMergeDirOverDevice(t *testing.T) {
	lower := layerTar(t, &tar.Header{Typeflag: tar.TypeChar, Name: "dir", Mode: 0o644})
	upper := layerTar(t, dirEntry("dir/"), fileEntry("dir/new"))
	fsys := openImage(t, buildMergedImage(t, lower, upper))
	checkOpaque(t, fsys, "dir", false)
	checkFile(t, fsys, "dir/new", "dir/new")
}

// recreatedDirTar is a layer that deletes dir and recreates it with new
// content: a whiteout for dir followed by the directory and its new file.
func recreatedDirTar(t *testing.T) []byte {
	t.Helper()
	return makeTar(t, func(tw *tar.Writer) {
		for _, h := range []*tar.Header{
			{Typeflag: tar.TypeReg, Name: ".wh.dir", Mode: 0o644},
			{Typeflag: tar.TypeDir, Name: "dir/", Mode: 0o755},
			{Typeflag: tar.TypeReg, Name: "dir/new", Size: 3, Mode: 0o644},
		} {
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := tw.Write([]byte("new")); err != nil {
			t.Fatal(err)
		}
	})
}

// TestConvertRecreatedDir verifies that a directory recreated after its own
// whiteout in the same layer becomes an opaque directory, which hides the
// lower layers' contents the way the whiteout and recreation together do.
func TestConvertRecreatedDir(t *testing.T) {
	fsys := openImage(t, buildImage(t, recreatedDirTar(t)))

	st, ok := checkStat(t, fsys, "dir").Sys().(*erofs.Stat)
	if !ok {
		t.Fatal("dir: no erofs.Stat")
	}
	if !checkStat(t, fsys, "dir").IsDir() {
		t.Fatal("dir must be a directory, not a whiteout")
	}
	if st.Xattrs["trusted.overlay.opaque"] != "y" {
		t.Errorf("dir must be opaque, got xattrs=%v", st.Xattrs)
	}
	checkFile(t, fsys, "dir/new", "new")
}

// TestMergeRecreatedDir verifies that in merge mode the recreated directory
// holds only the new layer's content.
func TestMergeRecreatedDir(t *testing.T) {
	lower := makeTar(t, func(tw *tar.Writer) {
		for _, h := range []*tar.Header{
			{Typeflag: tar.TypeDir, Name: "dir/", Mode: 0o755},
			{Typeflag: tar.TypeReg, Name: "dir/old", Mode: 0o644},
		} {
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
		}
	})
	fsys := openImage(t, buildMergedImage(t, lower, recreatedDirTar(t)))
	checkFile(t, fsys, "dir/new", "new")
	checkNotExist(t, fsys, "dir/old")
}
