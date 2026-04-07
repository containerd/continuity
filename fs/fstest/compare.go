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
	"fmt"
	"io/fs"
	"os"
)

// CheckDirectoryEqual compares two directory paths to make sure that
// the content of the directories is the same.
func CheckDirectoryEqual(d1, d2 string) error {
	r1, err := buildResourcesFS(os.DirFS(d1))
	if err != nil {
		return fmt.Errorf("failed to walk %s: %w", d1, err)
	}

	r2, err := buildResourcesFS(os.DirFS(d2))
	if err != nil {
		return fmt.Errorf("failed to walk %s: %w", d2, err)
	}

	diff := diffResourceList(r1, r2)
	if diff.HasDiff() {
		return fmt.Errorf("directory diff between %s and %s\n%s", d1, d2, diff.String())
	}

	return nil
}

// CheckDirectoryEqualWithApplier compares directory against applier
func CheckDirectoryEqualWithApplier(root string, a Applier) error {
	applied, err := os.MkdirTemp("", "fstest")
	if err != nil {
		return err
	}
	defer os.RemoveAll(applied)
	if err := a.Apply(applied); err != nil {
		return err
	}
	return CheckDirectoryEqual(applied, root)
}

// CheckDirectoryEqualFS compares two fs.FS implementations to make
// sure that the content of the file systems is the same.
//
// If an fs.FS also implements [fs.ReadLinkFS], symbolic link targets
// will be compared. Otherwise only the presence of symlinks is checked.
// Both fs.FS values should have the same fs.ReadLinkFS capability to
// avoid false differences from asymmetric symlink target data.
func CheckDirectoryEqualFS(fs1, fs2 fs.FS) error {
	r1, err := buildResourcesFS(fs1)
	if err != nil {
		return fmt.Errorf("failed to walk: %w", err)
	}

	r2, err := buildResourcesFS(fs2)
	if err != nil {
		return fmt.Errorf("failed to walk: %w", err)
	}

	diff := diffResourceList(r1, r2)
	if diff.HasDiff() {
		return fmt.Errorf("directory diff:\n%s", diff.String())
	}

	return nil
}
