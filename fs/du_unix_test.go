// +build !windows

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
	"syscall"

	"github.com/pkg/errors"
)

func getBsize(root string) (int64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(root, &s); err != nil {
		return 0, err
	}

	return int64(s.Bsize), nil // nolint: unconvert
}

func getTmpAlign() (func(int64) int64, error) {
	t1, err := ioutil.TempDir("", "compute-align-")
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temp dir")
	}
	defer os.RemoveAll(t1)

	bsize, err := getBsize(t1)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get bsize")
	}

	return func(size int64) int64 {
		// Align to blocks
		aligned := (size / bsize) * bsize

		// Add next block if has remainder
		if size%bsize > 0 {
			aligned += bsize
		}

		return aligned
	}, nil
}
