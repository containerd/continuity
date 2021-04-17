// +build linux darwin

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
	"io/ioutil"
	"os"
	"testing"

	"github.com/containerd/continuity/fs/fstest"
	"github.com/containerd/continuity/sysx"
)

func assertXAttr(t *testing.T, dir, xattr, xval string, xerr error) {
	t.Helper()
	value, err := sysx.Getxattr(dir, xattr)
	switch {
	case xerr == nil && err != nil:
		t.Errorf("err=%v; expected val=%s", err, xval)
	case xerr == nil && err == nil && string(value) != xval:
		t.Errorf("val=%s; expected val=%s", value, xval)
	case xerr != nil && err != xerr:
		t.Errorf("val=%s, err=%v; expected err=%v", value, err, xerr)
	}
}

func TestCopyDirWithXAttrExcludes(t *testing.T) {
	src, err := ioutil.TempDir("", "test-copy-src-with-xattr-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(src)

	if err := fstest.Apply(
		fstest.SetXAttr(".", "user.test-1", "one"),
		fstest.SetXAttr(".", "user.test-2", "two"),
		fstest.SetXAttr(".", "user.test-x", "three-four-five"),
	).Apply(src); err != nil {
		t.Fatal(err)
	}

	t.Run("none", func(t *testing.T) {
		dst, err := ioutil.TempDir("", "test-copy-dst-with-xattr-exclude-none-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dst)
		err = CopyDir(dst, src, WithXAttrExclude())
		if err != nil {
			t.Fatal(err)
		}
		assertXAttr(t, dst, "user.test-1", "one", nil)
		assertXAttr(t, dst, "user.test-2", "two", nil)
		assertXAttr(t, dst, "user.test-x", "three-four-five", nil)
	})

	t.Run("some", func(t *testing.T) {
		dst, err := ioutil.TempDir("", "test-copy-dst-with-xattr-exclude-some-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dst)
		err = CopyDir(dst, src, WithXAttrExclude("user.test-x"))
		if err != nil {
			t.Fatal(err)
		}
		assertXAttr(t, dst, "user.test-1", "one", nil)
		assertXAttr(t, dst, "user.test-2", "two", nil)
		assertXAttr(t, dst, "user.test-x", "", sysx.ENODATA)
	})
}
