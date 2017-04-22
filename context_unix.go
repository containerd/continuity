// +build linux darwin

package continuity

import (
	"fmt"
	"os"
	"syscall"
)

// getUidGidFromFileInfo extracts the user and group IDs from a fileinfo.
// This is Unix-specific functionality.
func getUidGidFromFileInfo(fi os.FileInfo) (uint32, uint32, error) {
	// TODO(stevvooe): This need to be resolved for the container's root,
	// where here we are really getting the host OS's value. We need to allow
	// this be passed in and fixed up to make these uid/gid mappings portable.
	// Either this can be part of the driver or we can achieve it through some
	// other mechanism.
	sys, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		// TODO(stevvooe): This may not be a hard error for all platforms. We
		// may want to move this to the driver.
		return 0, 0, fmt.Errorf("unable to resolve syscall.Stat_t from (os.FileInfo).Sys(): %#v", fi)
	}
	return sys.Uid, sys.Gid, nil
}
