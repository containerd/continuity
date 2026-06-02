package tarconv_test

// Benchmarks for tar.Convert and tar.Merge, with optional comparison against
// mkfs.erofs when it is present in PATH.
//
// Four synthetic workloads model real container image shapes:
//
//   Small  – ~200 entries, ~1MB data.   Typical Alpine base layer.
//   Medium – ~1000 entries, ~10MB data. Python/Node package install layer.
//   Large  – ~5000 entries, ~50MB data. Large app or source-tree layer.
//   Huge   – ~500 entries, ~100MB data. A few large binary files; exercises
//             raw I/O throughput where per-process spawn overhead is ~0%.
//
// Each workload runs:
//   BenchmarkConvert/<size>       – tar.Convert (layer mode)
//   BenchmarkMerge/<size>         – tar.Merge (merge mode, 2-layer scenario)
//   BenchmarkMkfsConvert/<size>   – mkfs.erofs --tar=f (skipped if not in PATH)
//
// Run with (recommended for stable numbers):
//   go test ./tar/... -bench=. -benchtime=10s -count=3 -benchmem

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	erofs "github.com/erofs/go-erofs"
	"github.com/containerd/continuity/tarconv"
)

// workload describes a set of synthetic tar entries to benchmark.
type workload struct {
	name    string
	entries func() []tarEntry
}

// tarEntry is a single entry to write into a tar.
type tarEntry struct {
	hdr  tar.Header
	data []byte
}

var benchEpoch = time.Unix(1700000000, 0)

// smallWorkload simulates an Alpine-like base layer (~200 entries, ~1MB data).
func smallWorkload() []tarEntry {
	return syntheticLayer(
		layerSpec{dirs: 20, filesPerDir: 5, fileSize: 1024, symlinks: 10, hardLinkFraction: 0.05},
	)
}

// mediumWorkload simulates a package install layer (~1000 entries, ~10MB data).
func mediumWorkload() []tarEntry {
	return syntheticLayer(
		layerSpec{dirs: 50, filesPerDir: 15, fileSize: 4096, symlinks: 50, hardLinkFraction: 0.1},
	)
}

// largeWorkload simulates a source-tree or large-app layer (~5000 entries, ~50MB data).
func largeWorkload() []tarEntry {
	return syntheticLayer(
		layerSpec{dirs: 100, filesPerDir: 40, fileSize: 8192, symlinks: 100, hardLinkFraction: 0.05},
	)
}

// hugeWorkload simulates a layer dominated by a few large binary files
// (~500 entries, ~100MB data). This eliminates per-process spawn overhead
// from the mkfs comparison and isolates raw I/O throughput.
func hugeWorkload() []tarEntry {
	return syntheticLayer(
		layerSpec{dirs: 20, filesPerDir: 20, fileSize: 256 * 1024, symlinks: 20, hardLinkFraction: 0.02},
	)
}

type layerSpec struct {
	dirs             int
	filesPerDir      int
	fileSize         int
	symlinks         int
	hardLinkFraction float64
}

// syntheticLayer generates a realistic tar layer according to spec.
func syntheticLayer(s layerSpec) []tarEntry {
	var entries []tarEntry

	// Root.
	entries = append(entries, tarEntry{hdr: tar.Header{
		Typeflag: tar.TypeDir, Name: "./", Mode: 0o755,
		Uid: 0, Gid: 0, ModTime: benchEpoch,
	}})

	// Standard directory skeleton.
	skeletonDirs := []string{"usr/", "usr/bin/", "usr/lib/", "usr/share/", "etc/", "var/", "var/log/", "tmp/"}
	for _, d := range skeletonDirs {
		entries = append(entries, tarEntry{hdr: tar.Header{
			Typeflag: tar.TypeDir, Name: d, Mode: 0o755,
			Uid: 0, Gid: 0, ModTime: benchEpoch,
		}})
	}

	// Generate payload data (reused across entries to avoid huge allocations).
	fileData := make([]byte, s.fileSize)
	for i := range fileData {
		fileData[i] = byte(i % 251)
	}

	var regularFiles []string

	for d := 0; d < s.dirs; d++ {
		dirName := fmt.Sprintf("pkg%04d/", d)
		entries = append(entries, tarEntry{hdr: tar.Header{
			Typeflag: tar.TypeDir, Name: dirName, Mode: 0o755,
			Uid: 1000, Gid: 1000, ModTime: benchEpoch,
		}})

		for f := 0; f < s.filesPerDir; f++ {
			name := fmt.Sprintf("%sfile%04d.dat", dirName, f)
			regularFiles = append(regularFiles, name)

			var pax map[string]string
			if f%10 == 0 {
				// Occasionally add an xattr (capabilities).
				pax = map[string]string{"SCHILY.xattr.security.capability": "\x01\x00\x00\x02\x00 \x00\x00"}
			}

			e := tarEntry{
				hdr: tar.Header{
					Typeflag:   tar.TypeReg,
					Name:       name,
					Size:       int64(s.fileSize),
					Mode:       0o644,
					Uid:        1000,
					Gid:        1000,
					ModTime:    benchEpoch,
					PAXRecords: pax,
				},
				data: fileData,
			}
			entries = append(entries, e)
		}
	}

	// Add hard links.
	hlCount := int(float64(len(regularFiles)) * s.hardLinkFraction)
	for i := 0; i < hlCount && i < len(regularFiles); i++ {
		target := regularFiles[i]
		linkName := fmt.Sprintf("links/hardlink%04d", i)
		// Ensure the links/ directory exists (add it once).
		if i == 0 {
			entries = append(entries, tarEntry{hdr: tar.Header{
				Typeflag: tar.TypeDir, Name: "links/", Mode: 0o755,
				Uid: 0, Gid: 0, ModTime: benchEpoch,
			}})
		}
		entries = append(entries, tarEntry{hdr: tar.Header{
			Typeflag: tar.TypeLink, Name: linkName, Linkname: target,
			Uid: 0, Gid: 0, ModTime: benchEpoch,
		}})
	}

	// Add symlinks.
	if len(regularFiles) > 0 {
		for i := 0; i < s.symlinks; i++ {
			target := regularFiles[i%len(regularFiles)]
			entries = append(entries, tarEntry{hdr: tar.Header{
				Typeflag: tar.TypeSymlink,
				Name:     fmt.Sprintf("symlinks/link%04d", i),
				Linkname: "/" + target,
				Mode:     0o777,
				ModTime:  benchEpoch,
			}})
		}
		// Make sure the symlinks/ dir was emitted first.
		symlinkDir := tarEntry{hdr: tar.Header{
			Typeflag: tar.TypeDir, Name: "symlinks/", Mode: 0o755,
			Uid: 0, Gid: 0, ModTime: benchEpoch,
		}}
		// Prepend before the symlink entries by splicing.
		// Find the first symlink entry index.
		firstSym := len(entries) - s.symlinks
		if firstSym < 0 {
			firstSym = 0
		}
		rest := make([]tarEntry, len(entries)-firstSym)
		copy(rest, entries[firstSym:])
		entries = append(entries[:firstSym], symlinkDir)
		entries = append(entries, rest...)
	}

	return entries
}

// buildTarBytes serialises entries to an in-memory tar.
func buildTarBytes(t testing.TB, entries []tarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	tw := tar.NewWriter(&out)
	for _, e := range entries {
		hdr := e.hdr // copy so we don't mutate
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", e.hdr.Name, err)
		}
		if len(e.data) > 0 {
			if _, err := tw.Write(e.data); err != nil {
				t.Fatalf("Write %s: %v", e.hdr.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	return out.Bytes()
}

// discardWriter is an io.WriteSeeker that discards output but tracks position.
type discardWriter struct{ pos int64 }

func (d *discardWriter) Write(p []byte) (int, error) { d.pos += int64(len(p)); return len(p), nil }
func (d *discardWriter) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		d.pos = offset
	case io.SeekCurrent:
		d.pos += offset
	case io.SeekEnd:
		// Not used by writer for the final seek.
		d.pos = offset
	}
	return d.pos, nil
}

// --- Benchmarks ---

var workloads = []workload{
	{"Small", smallWorkload},
	{"Medium", mediumWorkload},
	{"Large", largeWorkload},
	{"Huge", hugeWorkload},
}

// makeMergeLayer2 builds a second tar layer over the given base entries:
// 20% new files + whiteouts for every 20th regular file in the base.
func makeMergeLayer2(b testing.TB, base []tarEntry) []byte {
	b.Helper()
	var layer2 []tarEntry
	layer2 = append(layer2, tarEntry{hdr: tar.Header{
		Typeflag: tar.TypeDir, Name: "layer2/", Mode: 0o755, ModTime: benchEpoch,
	}})
	fileData := make([]byte, 512)
	for i := 0; i < len(base)/5; i++ {
		layer2 = append(layer2, tarEntry{
			hdr: tar.Header{
				Typeflag: tar.TypeReg,
				Name:     fmt.Sprintf("layer2/newfile%04d", i),
				Size:     int64(len(fileData)), Mode: 0o644, ModTime: benchEpoch,
			},
			data: fileData,
		})
	}
	for i, e := range base {
		if i%20 == 1 && e.hdr.Typeflag == tar.TypeReg {
			// Construct whiteout path: same directory, .wh. prefix on filename.
			p := e.hdr.Name
			slash := len(p) - 1
			for slash >= 0 && p[slash] != '/' {
				slash--
			}
			dir, name := p[:slash+1], p[slash+1:]
			layer2 = append(layer2, tarEntry{hdr: tar.Header{
				Typeflag: tar.TypeReg,
				Name:     dir + ".wh." + name,
				ModTime:  benchEpoch,
			}})
		}
	}
	return buildTarBytes(b, layer2)
}

// BenchmarkConvert benchmarks tarconv.Apply across all workload sizes.
// Reports throughput in MB/s of tar input processed.
func BenchmarkConvert(b *testing.B) {
	for _, wl := range workloads {
		wl := wl
		b.Run(wl.name, func(b *testing.B) {
			tarData := buildTarBytes(b, wl.entries())
			b.SetBytes(int64(len(tarData)))
			// Validate the image once before the timed loop.
			if b.N > 0 {
				dw := &buf{}
				w := erofs.Create(dw)
				if err := tarconv.Apply(w, bytes.NewReader(tarData)); err != nil {
					b.Fatalf("Convert (validation): %v", err)
				}
				if err := w.Close(); err != nil {
					b.Fatalf("Close (validation): %v", err)
				}
				fsckErofsBytes(b, dw.b)
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dw := &discardWriter{}
				w := erofs.Create(dw)
				if err := tarconv.Apply(w, bytes.NewReader(tarData)); err != nil {
					b.Fatalf("Convert: %v", err)
				}
				if err := w.Close(); err != nil {
					b.Fatalf("Close: %v", err)
				}
			}
		})
	}
}

// BenchmarkMerge benchmarks tarconv.Apply(WithMerge) (two-layer merge).
// Layer 1 is the base; layer 2 adds new files and whiteouts.
func BenchmarkMerge(b *testing.B) {
	for _, wl := range workloads {
		wl := wl
		b.Run(wl.name, func(b *testing.B) {
			base := wl.entries()
			layer1 := buildTarBytes(b, base)
			layer2 := makeMergeLayer2(b, base)
			b.SetBytes(int64(len(layer1) + len(layer2)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				dw := &discardWriter{}
				w := erofs.Create(dw)
			if err := tarconv.Apply(w, bytes.NewReader(layer1), tarconv.WithMerge()); err != nil {
				b.Fatalf("Apply(WithMerge) layer1: %v", err)
			}
			if err := tarconv.Apply(w, bytes.NewReader(layer2), tarconv.WithMerge()); err != nil {
				b.Fatalf("Apply(WithMerge) layer2: %v", err)
			}
				if err := w.Close(); err != nil {
					b.Fatalf("Close: %v", err)
				}
			}
		})
	}
}

// BenchmarkMkfsConvert benchmarks mkfs.erofs --tar=f as a reference point.
// Skipped if mkfs.erofs is not in PATH. Uses the same fixed timestamp as
// other tests for fair comparison. Reports throughput in MB/s of tar input.
//
// Note: mkfs.erofs writes to a real file on disk and has fork/exec overhead
// per iteration. The throughput figures will be lower than tar.Convert for
// small inputs due to spawn cost, but converge as tar size grows.
func BenchmarkMkfsConvert(b *testing.B) {
	if _, err := exec.LookPath("mkfs.erofs"); err != nil {
		b.Skip("mkfs.erofs not found in PATH")
	}
	for _, wl := range workloads {
		wl := wl
		b.Run(wl.name, func(b *testing.B) {
			tarData := buildTarBytes(b, wl.entries())
			outFile, err := os.CreateTemp("", "mkfs-bench-*.erofs")
			if err != nil {
				b.Fatal(err)
			}
			outPath := outFile.Name()
			_ = outFile.Close()
			defer os.Remove(outPath)

			b.SetBytes(int64(len(tarData)))
			b.ResetTimer()

			ctx := context.Background()
			for i := 0; i < b.N; i++ {
				if err := convertTarMkfs(ctx, b, tarData, outPath, nil); err != nil {
					b.Fatalf("mkfs.erofs: %v", err)
				}
				_ = os.Remove(outPath)
			}
		})
	}
}
