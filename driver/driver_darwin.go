package driver

import (
	"os"

	"github.com/containerd/continuity/devices"
)

func (d *driver) DeviceInfo(fi os.FileInfo) (maj uint64, min uint64, err error) {
	return devices.DeviceInfo(fi)
}
