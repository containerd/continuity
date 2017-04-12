// +build !linux

package continuity

import (
	"io"
	"os"
)

func fastCopy(dst *os.File, r io.Reader, size int64) (int64, error) {
	return 0, nil
}
