package fsdriver

import (
	"os"

	"github.com/containerd/continuity/device"
)

func (d *basicDriver) DeviceInfo(fi os.FileInfo) (maj uint64, min uint64, err error) {
	return device.DeviceInfo(fi)
}
