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
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/containerd/continuity/tarconv"
	erofs "github.com/erofs/go-erofs"
)

// oldGNUSparseTar returns a tar holding one old GNU sparse file (type 'S')
// named "sparse": five bytes of hole followed by "hello". archive/tar cannot
// write this format, so a regular GNU entry is rewritten in place.
func oldGNUSparseTar(t *testing.T) []byte {
	t.Helper()
	data := makeTar(t, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeReg, Name: "sparse", Size: 5, Mode: 0644, Format: tar.FormatGNU,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte("hello")); err != nil {
			t.Fatal(err)
		}
	})
	hdr := data[:512]
	octal := func(off, n int, v int64) {
		copy(hdr[off:off+n], fmt.Sprintf("%0*o\x00", n-1, v))
	}
	hdr[156] = tar.TypeGNUSparse
	octal(386, 12, 5)  // first sparse entry: data at offset 5
	octal(398, 12, 5)  // of length 5
	octal(483, 12, 10) // real size
	copy(hdr[148:156], "        ")
	var sum int64
	for _, b := range hdr {
		sum += int64(b)
	}
	copy(hdr[148:156], fmt.Sprintf("%06o\x00 ", sum))
	return data
}

// TestApplyOldGNUSparse verifies that an old GNU sparse file is written with
// its reconstructed contents rather than skipped.
func TestApplyOldGNUSparse(t *testing.T) {
	data := oldGNUSparseTar(t)
	want := "\x00\x00\x00\x00\x00hello"

	t.Run("full", func(t *testing.T) {
		checkFile(t, openImage(t, buildImage(t, data)), "sparse", want)
	})

	t.Run("tar index", func(t *testing.T) {
		var meta buf
		dev, err := os.CreateTemp(t.TempDir(), "data")
		if err != nil {
			t.Fatal(err)
		}
		defer dev.Close()
		w := erofs.Create(&meta, erofs.WithBlockSize(512), erofs.WithDataFile(dev))
		if err := tarconv.Apply(w, bytes.NewReader(data), tarconv.WithTarIndexData(dev)); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		fsys, err := erofs.Open(&meta, erofs.WithExtraDevices(dev))
		if err != nil {
			t.Fatal(err)
		}
		checkFile(t, fsys, "sparse", want)
	})
}
