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
	"io"
	"io/fs"
	"os"
	"testing"

	erofs "github.com/erofs/go-erofs"

	"github.com/containerd/continuity/tarconv"
)

// TestWithTarIndexDataBasic verifies that WithTarIndexData produces a valid
// EROFS metadata image with file-payload data stored in the external data file.
//
// The combined output (EROFS metadata + data file) is then opened with
// go-erofs using WithExtraDevices so that file content can be read back.
func TestWithTarIndexDataBasic(t *testing.T) {
	const fileContent = "hello tar-index world"

	// Build a simple tar with one directory and one file.
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	mustWriteHeader(t, tw, &tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0755})
	mustWriteHeader(t, tw, &tar.Header{
		Typeflag: tar.TypeReg, Name: "hello.txt",
		Size: int64(len(fileContent)), Mode: 0644,
	})
	if _, err := tw.Write([]byte(fileContent)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	// dataFile receives file payload bytes.
	dataFile, err := os.CreateTemp(t.TempDir(), "tarindex-data-*")
	if err != nil {
		t.Fatal(err)
	}
	defer dataFile.Close()

	// metaBuf holds the EROFS metadata image.
	var metaBuf buf

	// Block size 512 matches tar granularity.
	w := erofs.Create(&metaBuf, erofs.WithBlockSize(512), erofs.WithDataFile(dataFile))
	if err := tarconv.Apply(w, &tarBuf, tarconv.WithTarIndexData(dataFile)); err != nil {
		t.Fatalf("Apply (tar-index): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}

	// Verify: the EROFS metadata image has a valid superblock magic.
	if len(metaBuf.b) < 1028 {
		t.Fatalf("metadata image too small: %d bytes", len(metaBuf.b))
	}
	checkEROFSSuperblock(t, metaBuf.b)

	// Verify: opening the combined image with the data file as extra device
	// allows reading back the file content.
	if _, err := dataFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	metaRA := bytes.NewReader(metaBuf.b)
	fsys, err := erofs.Open(metaRA, erofs.WithExtraDevices(dataFile))
	if err != nil {
		t.Fatalf("erofs.Open: %v", err)
	}

	f, err := fsys.Open("hello.txt")
	if err != nil {
		t.Fatalf("open hello.txt: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read hello.txt: %v", err)
	}
	if string(got) != fileContent {
		t.Errorf("hello.txt content = %q, want %q", got, fileContent)
	}
}

// TestWithTarIndexDataFileListing verifies that the EROFS metadata image
// produced by WithTarIndexData contains the same file listing as a
// full-extraction image built from the same tar.
func TestWithTarIndexDataFileListing(t *testing.T) {
	tc := tarContext{}
	entries := tarAll{
		tc.dir("./", 0755),
		tc.dir("usr/", 0755),
		tc.dir("usr/bin/", 0755),
		tc.file("usr/bin/hello", []byte("#!/bin/sh\necho hello\n"), 0755),
		tc.file("etc/hostname", []byte("testhost\n"), 0644),
		tc.symlink("usr/bin/hi", "hello"),
	}

	// Full-extraction image.
	var fullBuf buf
	wFull := erofs.Create(&fullBuf)
	if err := tarconv.Apply(wFull, tarFromWriterTo(entries)); err != nil {
		t.Fatalf("Apply (full): %v", err)
	}
	if err := wFull.Close(); err != nil {
		t.Fatal(err)
	}

	// Tar-index image.
	dataFile, err := os.CreateTemp(t.TempDir(), "tarindex-data-*")
	if err != nil {
		t.Fatal(err)
	}
	defer dataFile.Close()

	var metaBuf buf
	wIdx := erofs.Create(&metaBuf, erofs.WithBlockSize(512), erofs.WithDataFile(dataFile))
	if err := tarconv.Apply(wIdx, tarFromWriterTo(entries), tarconv.WithTarIndexData(dataFile)); err != nil {
		t.Fatalf("Apply (tar-index): %v", err)
	}
	if err := wIdx.Close(); err != nil {
		t.Fatal(err)
	}

	// Compare file listings.
	fullNames := walkFS(t, bytes.NewReader(fullBuf.b))
	if _, err := dataFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	idxNames := walkFSWithDevice(t, bytes.NewReader(metaBuf.b), dataFile)

	if len(fullNames) != len(idxNames) {
		t.Errorf("file count mismatch: full=%d tar-index=%d\nfull: %v\nindex: %v",
			len(fullNames), len(idxNames), fullNames, idxNames)
		return
	}
	full := make(map[string]bool, len(fullNames))
	for _, n := range fullNames {
		full[n] = true
	}
	for _, n := range idxNames {
		if !full[n] {
			t.Errorf("tar-index has %q but full-extraction does not", n)
		}
	}
}

// TestWithTarIndexDataWhiteouts verifies that OCI whiteout entries are
// translated to overlayfs representation in tar-index mode.
func TestWithTarIndexDataWhiteouts(t *testing.T) {
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	mustWriteHeader(t, tw, &tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0755})
	mustWriteHeader(t, tw, &tar.Header{Typeflag: tar.TypeReg, Name: "keep.txt", Size: 4, Mode: 0644})
	if _, err := tw.Write([]byte("data")); err != nil {
		t.Fatal(err)
	}
	// Whiteout for "gone.txt".
	mustWriteHeader(t, tw, &tar.Header{Typeflag: tar.TypeReg, Name: ".wh.gone.txt", Size: 0, Mode: 0644})
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dataFile, err := os.CreateTemp(t.TempDir(), "tarindex-data-*")
	if err != nil {
		t.Fatal(err)
	}
	defer dataFile.Close()

	var metaBuf buf
	w := erofs.Create(&metaBuf, erofs.WithBlockSize(512), erofs.WithDataFile(dataFile))
	if err := tarconv.Apply(w, &tarBuf, tarconv.WithTarIndexData(dataFile)); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	checkEROFSSuperblock(t, metaBuf.b)

	if _, err := dataFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	names := walkFSWithDevice(t, bytes.NewReader(metaBuf.b), dataFile)

	var foundKeep, foundGone bool
	for _, n := range names {
		switch n {
		case "keep.txt":
			foundKeep = true
		case "gone.txt":
			foundGone = true
		}
	}
	if !foundKeep {
		t.Errorf("keep.txt must be present: %v", names)
	}
	if !foundGone {
		t.Errorf("gone.txt whiteout must appear as a device node: %v", names)
	}
}

// ---------------------------------------------------------------------------
// TestWithTarIndexDataXattrs verifies that file content is still readable when
// the chunk-index inodes also carry extended attributes.
//
// A chunk-based inode stores its chunk-index map after the inode header and the
// xattr area, at an offset aligned to the size of a chunk-index entry. Because
// the xattr body header is 12 bytes, an inode carrying xattrs can leave that
// boundary unaligned, so the writer has to insert padding that the reader (and
// the kernel) accounts for. Getting this wrong silently misreads every chunk
// index, so exercise xattrs of several lengths together with multi-block files.
func TestWithTarIndexDataXattrs(t *testing.T) {
	// Payloads deliberately span one, two and several 512-byte blocks so a
	// misread chunk index cannot go unnoticed.
	files := []struct {
		name    string
		content []byte
		xattrs  map[string]string
	}{
		{
			name:    "bin/ping",
			content: bytes.Repeat([]byte("p"), 300),
			xattrs: map[string]string{
				"security.capability": "\x01\x00\x00\x02\x00\x20\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00",
			},
		},
		{
			name:    "bin/multi",
			content: bytes.Repeat([]byte("m"), 3*512+17),
			xattrs:  map[string]string{"user.a": "1"},
		},
		{
			name:    "bin/many",
			content: bytes.Repeat([]byte("n"), 9*512),
			xattrs: map[string]string{
				"user.one":   "first",
				"user.two":   "second",
				"user.three": "third",
			},
		},
		{
			// No xattrs: the aligned baseline for comparison.
			name:    "bin/plain",
			content: bytes.Repeat([]byte("q"), 2*512),
		},
	}

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	mustWriteHeader(t, tw, &tar.Header{Typeflag: tar.TypeDir, Name: "./", Mode: 0755})
	mustWriteHeader(t, tw, &tar.Header{Typeflag: tar.TypeDir, Name: "bin/", Mode: 0755})
	for _, f := range files {
		hdr := &tar.Header{
			Typeflag: tar.TypeReg, Name: f.name,
			Size: int64(len(f.content)), Mode: 0755,
		}
		if len(f.xattrs) > 0 {
			hdr.PAXRecords = make(map[string]string, len(f.xattrs))
			for k, v := range f.xattrs {
				hdr.PAXRecords["SCHILY.xattr."+k] = v
			}
		}
		mustWriteHeader(t, tw, hdr)
		if _, err := tw.Write(f.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dataFile, err := os.CreateTemp(t.TempDir(), "tarindex-xattr-data-*")
	if err != nil {
		t.Fatal(err)
	}
	defer dataFile.Close()

	var metaBuf buf
	w := erofs.Create(&metaBuf, erofs.WithBlockSize(512), erofs.WithDataFile(dataFile))
	if err := tarconv.Apply(w, &tarBuf, tarconv.WithTarIndexData(dataFile)); err != nil {
		t.Fatalf("Apply (tar-index): %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Writer.Close: %v", err)
	}
	checkEROFSSuperblock(t, metaBuf.b)

	if _, err := dataFile.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	fsys, err := erofs.Open(bytes.NewReader(metaBuf.b), erofs.WithExtraDevices(dataFile))
	if err != nil {
		t.Fatalf("erofs.Open: %v", err)
	}

	for _, f := range files {
		got, err := fs.ReadFile(fsys, f.name)
		if err != nil {
			t.Errorf("ReadFile %s: %v", f.name, err)
			continue
		}
		// Compare byte-exactly; a misaligned chunk-index map typically yields
		// correctly-sized but wrong content.
		if !bytes.Equal(got, f.content) {
			t.Errorf("%s: content mismatch (got %d bytes, want %d bytes)", f.name, len(got), len(f.content))
			continue
		}
		info, err := fs.Stat(fsys, f.name)
		if err != nil {
			t.Errorf("Stat %s: %v", f.name, err)
			continue
		}
		st, ok := info.Sys().(*erofs.Stat)
		if !ok {
			t.Errorf("%s: Sys() = %T, want *erofs.Stat", f.name, info.Sys())
			continue
		}
		for k, want := range f.xattrs {
			if got := st.Xattrs[k]; got != want {
				t.Errorf("%s: xattr %s = %q, want %q", f.name, k, got, want)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// checkEROFSSuperblock verifies the EROFS magic at offset 1024 of data.
func checkEROFSSuperblock(t *testing.T, data []byte) {
	t.Helper()
	if len(data) < 1028 {
		t.Fatalf("EROFS image too small (%d bytes) for superblock", len(data))
	}
	magic := data[1024:1028]
	// EROFS magic 0xE0F5E1E2 little-endian.
	if magic[0] != 0xE2 || magic[1] != 0xE1 || magic[2] != 0xF5 || magic[3] != 0xE0 {
		t.Errorf("invalid EROFS superblock magic at offset 1024: %02x %02x %02x %02x",
			magic[0], magic[1], magic[2], magic[3])
	}
}

// walkFS opens an EROFS image and returns all file paths.
func walkFS(t *testing.T, ra io.ReaderAt) []string {
	t.Helper()
	fsys, err := erofs.Open(ra)
	if err != nil {
		t.Fatalf("erofs.Open: %v", err)
	}
	return walkFSys(t, fsys)
}

// walkFSWithDevice opens an EROFS image with an extra data device and returns all paths.
func walkFSWithDevice(t *testing.T, ra io.ReaderAt, dev io.ReaderAt) []string {
	t.Helper()
	fsys, err := erofs.Open(ra, erofs.WithExtraDevices(dev))
	if err != nil {
		t.Fatalf("erofs.Open (with device): %v", err)
	}
	return walkFSys(t, fsys)
}

func walkFSys(t *testing.T, fsys fs.FS) []string {
	t.Helper()
	var names []string
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, p)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	return names
}

// mustWriteHeader calls tw.WriteHeader and fatals on error.
func mustWriteHeader(t *testing.T, tw *tar.Writer, hdr *tar.Header) {
	t.Helper()
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader %s: %v", hdr.Name, err)
	}
}
