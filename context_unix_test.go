//go:build !windows

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

package continuity

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestContextWalkSymlinkRoot(t *testing.T) {
	root := t.TempDir()

	target := filepath.Join(root, "target")
	if err := os.MkdirAll(filepath.Join(target, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "subdir", "file"), []byte("content"), 0o644); err != nil {
		t.Fatal(err)
	}

	links := filepath.Join(root, "links")
	if err := os.Mkdir(links, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(links, "root")
	if err := os.Symlink("../target", link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	absoluteLink := filepath.Join(links, "absolute-root")
	if err := os.Symlink(target, absoluteLink); err != nil {
		t.Skipf("absolute symlink not supported: %v", err)
	}
	chain := filepath.Join(links, "chain")
	if err := os.Symlink("root", chain); err != nil {
		t.Skipf("symlink-to-symlink not supported: %v", err)
	}

	for _, tc := range []struct {
		name string
		root string
	}{
		{
			name: "relative_symlink",
			root: link,
		},
		{
			name: "absolute_symlink",
			root: absoluteLink,
		},
		{
			name: "symlink_to_symlink",
			root: chain,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := NewContext(tc.root)
			if err != nil {
				t.Fatal(err)
			}

			var got []string
			err = ctx.Walk(func(p string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if fi == nil {
					return fmt.Errorf("missing file info for %q", p)
				}
				got = append(got, filepath.ToSlash(p))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}

			want := []string{"/", "/subdir", "/subdir/file"}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("paths mismatch\nwant: %v\n got: %v", want, got)
			}
		})
	}
}
