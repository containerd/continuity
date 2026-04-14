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
	"regexp"
	"sort"
	"strings"
	"testing"
)

func checkTools(t testing.TB) {
	t.Helper()
	for _, tool := range []string{"mkfs.ext4", "debugfs", "e2fsck", "dumpe2fs"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not found in PATH", tool)
		}
	}
}

func fsck(t testing.TB, imgPath string) {
	t.Helper()
	cmd := exec.Command("e2fsck", "-nf", imgPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("e2fsck failed (filesystem corrupt): %v\n%s", err, out)
	}
}

func dumpSuperblock(t testing.TB, imgPath string) string {
	t.Helper()
	out, err := exec.Command("dumpe2fs", "-h", imgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("dumpe2fs failed: %v\n%s", err, out)
	}
	return string(out)
}

func dumpField(t testing.TB, dump, field string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(field) + `:\s*(.+)$`)
	m := re.FindStringSubmatch(dump)
	if m == nil {
		t.Fatalf("field %q not found in dumpe2fs output", field)
	}
	return strings.TrimSpace(m[1])
}

// normalizeFeatures sorts features and removes metadata_csum_seed which
// varies by mkfs.ext4 version.
func normalizeFeatures(s string) string {
	var feats []string
	for _, f := range strings.Fields(s) {
		if f != "metadata_csum_seed" {
			feats = append(feats, f)
		}
	}
	sort.Strings(feats)
	return strings.Join(feats, " ")
}

func debugfsStat(t testing.TB, imgPath, path string) string {
	t.Helper()
	out, err := exec.Command("debugfs", "-R", "stat "+path, imgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("debugfs stat %q failed: %v\n%s", path, err, out)
	}
	return string(out)
}

func debugfsLS(t testing.TB, imgPath, dir string) string {
	t.Helper()
	out, err := exec.Command("debugfs", "-R", "ls "+dir, imgPath).CombinedOutput()
	if err != nil {
		t.Fatalf("debugfs ls %q failed: %v\n%s", dir, err, out)
	}
	return string(out)
}

func TestCreate(t *testing.T) {
	checkTools(t)

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.ext4")

	if err := Create(imgPath, 64*1024*1024); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	info, err := os.Stat(imgPath)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if info.Size() != 64*1024*1024 {
		t.Fatalf("expected size 64 MiB, got %d", info.Size())
	}

	fsck(t, imgPath)

	dump := dumpSuperblock(t, imgPath)

	if bs := dumpField(t, dump, "Block size"); bs != "4096" {
		t.Errorf("expected block size 4096, got %s", bs)
	}

	features := dumpField(t, dump, "Filesystem features")
	if strings.Contains(features, "has_journal") {
		t.Error("expected journal to be disabled")
	}
	if !strings.Contains(features, "sparse_super") {
		t.Error("expected sparse_super feature")
	}
	if !strings.Contains(features, "metadata_csum") {
		t.Error("expected metadata_csum feature")
	}

	if rc := dumpField(t, dump, "Reserved block count"); rc != "0" {
		t.Errorf("expected 0 reserved blocks, got %s", rc)
	}

	lsOut := debugfsLS(t, imgPath, "/")
	if !strings.Contains(lsOut, "lost+found") {
		t.Errorf("expected lost+found in root listing:\n%s", lsOut)
	}

	lpfStat := debugfsStat(t, imgPath, "lost+found")
	if !strings.Contains(lpfStat, "directory") {
		t.Errorf("expected lost+found to be a directory:\n%s", lpfStat)
	}
}

func TestCreateSizes(t *testing.T) {
	checkTools(t)

	sizes := []int64{
		64 * 1024 * 1024,
		256 * 1024 * 1024,
		1024 * 1024 * 1024,
	}

	for _, size := range sizes {
		name := fmt.Sprintf("%dMiB", size/(1024*1024))
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			imgPath := filepath.Join(dir, "test.ext4")

			if err := Create(imgPath, size); err != nil {
				t.Fatalf("Create failed: %v", err)
			}

			info, err := os.Stat(imgPath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != size {
				t.Errorf("expected apparent size %d, got %d", size, info.Size())
			}

			fsck(t, imgPath)

			dump := dumpSuperblock(t, imgPath)
			if rc := dumpField(t, dump, "Reserved block count"); rc != "0" {
				t.Errorf("expected 0 reserved blocks, got %s", rc)
			}
		})
	}
}

func TestCreateMatchesMkfs(t *testing.T) {
	checkTools(t)

	dir := t.TempDir()
	nativePath := filepath.Join(dir, "native.ext4")
	mkfsPath := filepath.Join(dir, "mkfs.ext4")

	if err := Create(nativePath, 64*1024*1024); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	f, _ := os.Create(mkfsPath)
	f.Truncate(64 * 1024 * 1024)
	f.Close()
	cmd := exec.Command("mkfs.ext4", "-b", "4096", "-m", "0",
		"-O", "^has_journal,sparse_super2,^resize_inode",
		"-E", "lazy_itable_init=1,lazy_journal_init=1,nodiscard,assume_storage_prezeroed=1",
		"-F", "-q", mkfsPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mkfs.ext4 failed: %v\n%s", err, out)
	}

	fsck(t, nativePath)
	fsck(t, mkfsPath)

	nDump := dumpSuperblock(t, nativePath)
	mDump := dumpSuperblock(t, mkfsPath)

	fieldsToCompare := []string{
		"Block size",
		"Block count",
		"Reserved block count",
		"Free blocks",
		"Free inodes",
		"Inode count",
		"Blocks per group",
		"Inodes per group",
		"Inode blocks per group",
		"Overhead clusters",
		"Filesystem features",
		"Default mount options",
		"Inode size",
		"Group descriptor size",
	}

	for _, field := range fieldsToCompare {
		nVal := dumpField(t, nDump, field)
		mVal := dumpField(t, mDump, field)
		if field == "Filesystem features" {
			// Compare as sorted sets; metadata_csum_seed availability
			// varies by mkfs.ext4 version so allow it to differ.
			nVal = normalizeFeatures(nVal)
			mVal = normalizeFeatures(mVal)
		}
		if nVal != mVal {
			t.Errorf("field %q: native=%q mkfs=%q", field, nVal, mVal)
		}
	}
}

func TestCreateWithDirs(t *testing.T) {
	checkTools(t)

	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.ext4")

	err := Create(imgPath, 64*1024*1024,
		WithDir("/data", 0755, 1000, 1000),
		WithDir("/data/subdir", 0700, 0, 0),
		WithDir("/var/log", 0755, 0, 0),
	)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	fsck(t, imgPath)

	expect := []struct {
		path string
		uid  string
		gid  string
	}{
		{"/data", "1000", "1000"},
		{"/data/subdir", "0", "0"},
		{"/var/log", "0", "0"},
	}

	uidRe := regexp.MustCompile(`User:\s*(\d+)`)
	gidRe := regexp.MustCompile(`Group:\s*(\d+)`)

	for _, e := range expect {
		statOut := debugfsStat(t, imgPath, strings.TrimPrefix(e.path, "/"))

		if !strings.Contains(statOut, "directory") {
			t.Errorf("%s: expected directory type in stat output:\n%s", e.path, statOut)
		}
		if m := uidRe.FindStringSubmatch(statOut); m == nil || m[1] != e.uid {
			t.Errorf("%s: expected UID %s, got %v", e.path, e.uid, m)
		}
		if m := gidRe.FindStringSubmatch(statOut); m == nil || m[1] != e.gid {
			t.Errorf("%s: expected GID %s, got %v", e.path, e.gid, m)
		}
	}

	// Verify intermediate /var inherits perms from /var/log
	varStat := debugfsStat(t, imgPath, "var")
	if m := uidRe.FindStringSubmatch(varStat); m == nil || m[1] != "0" {
		t.Errorf("/var: expected UID 0 (inherited from /var/log), got %v", m)
	}
	if m := gidRe.FindStringSubmatch(varStat); m == nil || m[1] != "0" {
		t.Errorf("/var: expected GID 0 (inherited from /var/log), got %v", m)
	}

	lsOut := debugfsLS(t, imgPath, "/")
	for _, name := range []string{"data", "var", "lost+found"} {
		if !strings.Contains(lsOut, name) {
			t.Errorf("expected %q in root listing:\n%s", name, lsOut)
		}
	}

	lsData := debugfsLS(t, imgPath, "/data")
	if !strings.Contains(lsData, "subdir") {
		t.Errorf("expected subdir in /data listing:\n%s", lsData)
	}
}

func TestCreateInvalidSize(t *testing.T) {
	dir := t.TempDir()
	imgPath := filepath.Join(dir, "test.ext4")

	if err := Create(imgPath, 0); err == nil {
		t.Fatal("expected error for zero size")
	}
	if err := Create(imgPath, -1); err == nil {
		t.Fatal("expected error for negative size")
	}
	if err := Create(imgPath, 1024*1024); err == nil {
		t.Fatal("expected error for too-small size")
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"/", nil},
		{"/foo", []string{"foo"}},
		{"/foo/bar", []string{"foo", "bar"}},
		{"foo/bar/baz", []string{"foo", "bar", "baz"}},
		{"/foo/", []string{"foo"}},
	}
	for _, tt := range tests {
		got := splitPath(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("splitPath(%q) = %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitPath(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}
