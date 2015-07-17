package continuity

import "os"

var (
	devNullResource = resource{
		kind: chardev,
		path: "null",
		// TODO(stevvooe): These are system specific.
		major: 3,
		minor: 2,
		mode:  0666 | os.ModeDevice | os.ModeCharDevice,
	}

	devZeroResource = resource{
		kind: chardev,
		path: "zero",
		// TODO(stevvooe): These are system specific.
		major: 3,
		minor: 3,
		mode:  0666 | os.ModeDevice | os.ModeCharDevice,
	}
)
