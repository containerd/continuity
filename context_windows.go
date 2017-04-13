package continuity

import "os"

// getUidGidFromFileInfo extracts the user and group IDs from a fileinfo.
// This is Unix-specific functionality.
func getUidGidFromFileInfo(fi os.FileInfo) (uint32, uint32, error) {
	return 0, 0, nil
}
