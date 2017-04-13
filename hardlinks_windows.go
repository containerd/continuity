package continuity

import "os"

// NOTE(stevvooe): Obviously, this is not yet implemented. However, the
// makings of an implementation are available in src/os/types_windows.go. More
// investigation needs to be done to figure out exactly how to do this.

// hardlinkKey provides a tuple-key for managing hardlinks. This is system-
// specific.
type hardlinkKey struct {
}

// newHardlinkKey returns a hardlink key for the provided file info. If the
// resource does not represent a possible hardlink, errNotAHardLink will be
// returned.
func newHardlinkKey(fi os.FileInfo) (hardlinkKey, error) {
	return hardlinkKey{}, errNotHardLinkImplemented
}
