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
