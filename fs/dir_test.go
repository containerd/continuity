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
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDirReader(t *testing.T) {
	t.Run("empty dir", func(t *testing.T) {
		t.Parallel()

		dr := newTestDirReader(t, nil)
		if dr.Next() != nil {
			t.Fatal("expected nil dir entry for empty dir")
		}
		// validate that another call will still be nil and not panic
		if dr.Next() != nil {
			t.Fatal("expected nil dir entry for empty dir")
		}
	})

	t.Run("populated dir", func(t *testing.T) {
		t.Parallel()

		content := map[string]*testFile{
			"foo":     newTestFile([]byte("hello"), 0644),
			"bar/baz": newTestFile([]byte("world"), 0600),
			"bar":     newTestFile(nil, os.ModeDir|0710),
		}
		found := make(map[string]bool, len(content))
		shouldSkip := map[string]bool{
			"bar/baz": true,
		}
		dr := newTestDirReader(t, content)

		check := func(entry os.DirEntry) {
			tf := content[entry.Name()]
			if tf == nil {
				t.Errorf("got unknown entry: %s", entry)
				return
			}

			fi, err := entry.Info()
			if err != nil {
				t.Error()
				return
			}

			// Windows file permissions are not accurately represented in mode like this and will show 0666 (files) and 0777 (dirs)
			// As such, do not try to compare mode equality
			var modeOK bool
			if runtime.GOOS == "windows" {
				modeOK = fi.Mode().IsRegular() == tf.mode.IsRegular() && fi.Mode().IsDir() == tf.mode.IsDir()
			} else {
				modeOK = fi.Mode() == tf.mode
			}
			if !modeOK {
				t.Errorf("%s: file modes do not match, expected: %s, got: %s", fi.Name(), tf.mode, fi.Mode())
			}

			if fi.Mode().IsRegular() {
				dt, err := os.ReadFile(filepath.Join(dr.f.Name(), entry.Name()))
				if err != nil {
					t.Error(err)
					return
				}
				if !bytes.Equal(tf.dt, dt) {
					t.Errorf("expected %q, got: %q", string(tf.dt), string(dt))
				}
			}
		}

		for {
			entry := dr.Next()
			if entry == nil {
				break
			}
			found[entry.Name()] = true
			check(entry)
		}

		if err := dr.Err(); err != nil {
			t.Fatal(err)
		}

		if len(found) != len(content)-len(shouldSkip) {
			t.Fatalf("exected files [%s], got: [%s]", mapToStringer(content), mapToStringer(found))
		}
		for k := range shouldSkip {
			if found[k] {
				t.Errorf("expected dir reader to skip %s", k)
			}
		}
	})
}

type stringerFunc func() string

func (f stringerFunc) String() string {
	return f()
}

func mapToStringer[T any](in map[string]T) stringerFunc {
	return func() string {
		out := make([]string, 0, len(in))
		for k := range in {
			out = append(out, k)
		}
		return strings.Join(out, ",")
	}
}

type testFile struct {
	dt   []byte
	mode os.FileMode
}

func newTestFile(dt []byte, mode os.FileMode) *testFile {
	return &testFile{
		dt:   dt,
		mode: mode,
	}
}

func newTestDirReader(t *testing.T, content map[string]*testFile) *dirReader {
	p := t.TempDir()

	for cp, info := range content {
		fp := filepath.Join(p, cp)

		switch {
		case info.mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(fp), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(fp, info.dt, info.mode.Perm()); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(fp, info.mode.Perm()); err != nil {
				t.Fatal(err)
			}
		case info.mode.IsDir():
			if err := os.MkdirAll(fp, info.mode); err != nil {
				t.Fatal(err)
			}
			// make sure the dir has the right perms in case it was created earlier while writing a file
			if err := os.Chmod(fp, info.mode.Perm()); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("unexpected file mode")
		}
	}

	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return &dirReader{f: f}
}
