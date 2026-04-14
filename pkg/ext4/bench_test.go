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

package ext4

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func diskUsage(t testing.TB, path string) (apparent, actual int64) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	apparent = info.Size()

	out, err := exec.Command("du", "--block-size=1", path).CombinedOutput()
	if err == nil {
		fmt.Sscanf(string(out), "%d", &actual)
	} else {
		actual = apparent
	}
	return apparent, actual
}

var benchSizes = []struct {
	name string
	size int64
}{
	{"64MiB", 64 * 1024 * 1024},
	{"256MiB", 256 * 1024 * 1024},
	{"1GiB", 1024 * 1024 * 1024},
}

// BenchmarkCreate benchmarks the native Go Create function.
func BenchmarkCreate(b *testing.B) {
	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			outDir := b.TempDir()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				imgPath := filepath.Join(outDir, fmt.Sprintf("test_%d.ext4", i))
				if err := Create(imgPath, sz.size); err != nil {
					b.Fatal(err)
				}
				if i == b.N-1 {
					apparent, actual := diskUsage(b, imgPath)
					b.ReportMetric(float64(apparent), "apparent-bytes")
					b.ReportMetric(float64(actual), "disk-bytes")
				}
			}
		})
	}
}

// BenchmarkMkfsOptimized benchmarks mkfs.ext4 with the same optimized flags.
func BenchmarkMkfsOptimized(b *testing.B) {
	checkTools(b)

	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			outDir := b.TempDir()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				imgPath := filepath.Join(outDir, fmt.Sprintf("test_%d.ext4", i))
				f, err := os.Create(imgPath)
				if err != nil {
					b.Fatal(err)
				}
				f.Truncate(sz.size)
				f.Close()
				cmd := exec.Command("mkfs.ext4",
					"-b", "4096", "-m", "0",
					"-O", "^has_journal,sparse_super2,^resize_inode",
					"-E", "lazy_itable_init=1,lazy_journal_init=1,nodiscard,assume_storage_prezeroed=1",
					"-F", "-q", imgPath)
				if out, err := cmd.CombinedOutput(); err != nil {
					b.Fatalf("mkfs.ext4 failed: %v\n%s", err, out)
				}
				if i == b.N-1 {
					apparent, actual := diskUsage(b, imgPath)
					b.ReportMetric(float64(apparent), "apparent-bytes")
					b.ReportMetric(float64(actual), "disk-bytes")
				}
			}
		})
	}
}

// BenchmarkMkfsDefaults benchmarks mkfs.ext4 with default options.
func BenchmarkMkfsDefaults(b *testing.B) {
	checkTools(b)

	for _, sz := range benchSizes {
		b.Run(sz.name, func(b *testing.B) {
			outDir := b.TempDir()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				imgPath := filepath.Join(outDir, fmt.Sprintf("test_%d.ext4", i))
				f, err := os.Create(imgPath)
				if err != nil {
					b.Fatal(err)
				}
				f.Truncate(sz.size)
				f.Close()
				cmd := exec.Command("mkfs.ext4", "-F", "-q", imgPath)
				if out, err := cmd.CombinedOutput(); err != nil {
					b.Fatalf("mkfs.ext4 failed: %v\n%s", err, out)
				}
				if i == b.N-1 {
					apparent, actual := diskUsage(b, imgPath)
					b.ReportMetric(float64(apparent), "apparent-bytes")
					b.ReportMetric(float64(actual), "disk-bytes")
				}
			}
		})
	}
}
