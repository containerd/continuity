// +build linux

package fs

import (
	"syscall"
	"testing"

	"os"
	"path/filepath"

	"github.com/containerd/continuity/fs/fstest"
	"gotest.tools/assert"
)

func getCBFunc(t *testing.T, copyMode Mode) func(string, string) {
	return func(srcDir string, dstDir string) {
		srcPath := filepath.Join(srcDir, "nothing.txt")
		dstPath := filepath.Join(dstDir, "nothing.txt")
		srcFileInfo, err := os.Stat(srcPath)
		assert.NilError(t, err)
		dstFileInfo, err := os.Stat(dstPath)
		assert.NilError(t, err)
		dstStat := dstFileInfo.Sys().(*syscall.Stat_t)
		srcStat := srcFileInfo.Sys().(*syscall.Stat_t)
		if copyMode == Hardlink {
			assert.Equal(t, srcStat.Ino, dstStat.Ino)
			assert.Equal(t, srcStat.Dev, dstStat.Dev)
		} else {
			assert.Assert(t, srcStat.Ino != dstStat.Ino)
			assert.Assert(t, srcStat.Ino != dstStat.Ino)
		}
	}
}

func TestCopyHardlinkMode(t *testing.T) {
	apply := fstest.Apply(
		fstest.CreateFile("nothing.txt", []byte{0x00, 0x00}, 0755),
	)

	cb := getCBFunc(t, Hardlink)

	if err := testCopy(apply, Hardlink, true, cb); err != nil {
		t.Fatalf("Hardlink test failed: %+v", err)
	}
}

func TestCopyNotHardlinkMode(t *testing.T) {
	apply := fstest.Apply(
		fstest.CreateFile("nothing.txt", []byte{0x00, 0x00}, 0755),
	)

	cb := getCBFunc(t, Content)

	if err := testCopy(apply, Content, true, cb); err != nil {
		t.Fatalf("Not hardlink test failed: %+v", err)
	}
}
