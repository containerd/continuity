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
	"crypto/sha256"
	"io"
	"io/fs"
	"sort"
)

// resource represents a filesystem entry for directory comparison.
type resource struct {
	path   string
	paths  []string // all paths, including hardlinks (sorted)
	mode   fs.FileMode
	uid    int64
	gid    int64
	size   int64
	sha256 [sha256.Size]byte // regular files only
	target string            // symlinks only
	major  uint64            // devices only
	minor  uint64            // devices only
}

// buildResourcesFS walks fsys and returns a sorted list of resources.
func buildResourcesFS(fsys fs.FS) ([]resource, error) {
	hardlinks := map[hardlinkKey]int{}
	var entries []resource

	rlfs, hasReadLink := fsys.(fs.ReadLinkFS)

	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			return err
		}

		r := resource{
			path: p,
			mode: fi.Mode(),
		}
		statResource(fi, &r)

		switch {
		case fi.Mode().IsRegular():
			r.size = fi.Size()

			// Check for hardlink before hashing to avoid redundant I/O.
			if key, ok := getHardlinkKey(fi); ok {
				if idx, exists := hardlinks[key]; exists {
					entries[idx].paths = append(entries[idx].paths, p)
					return nil
				}
				hardlinks[key] = len(entries)
			}

			h, err := hashFileFS(fsys, p)
			if err != nil {
				return err
			}
			r.sha256 = h

		case fi.Mode()&fs.ModeSymlink != 0:
			if hasReadLink {
				target, err := rlfs.ReadLink(p)
				if err != nil {
					return err
				}
				r.target = target
			}

		case fi.Mode()&fs.ModeDevice != 0:
			r.major, r.minor = getDeviceInfo(fi)
			if key, ok := getHardlinkKey(fi); ok {
				if idx, exists := hardlinks[key]; exists {
					entries[idx].paths = append(entries[idx].paths, p)
					return nil
				}
				hardlinks[key] = len(entries)
			}

		case fi.Mode()&fs.ModeNamedPipe != 0:
			if key, ok := getHardlinkKey(fi); ok {
				if idx, exists := hardlinks[key]; exists {
					entries[idx].paths = append(entries[idx].paths, p)
					return nil
				}
				hardlinks[key] = len(entries)
			}
		}

		r.paths = []string{p}
		entries = append(entries, r)
		return nil
	})
	if err != nil {
		return nil, err
	}

	for i := range entries {
		sort.Strings(entries[i].paths)
		entries[i].path = entries[i].paths[0]
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].path < entries[j].path
	})

	return entries, nil
}

func hashFileFS(fsys fs.FS, name string) ([sha256.Size]byte, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [sha256.Size]byte{}, err
	}
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}
