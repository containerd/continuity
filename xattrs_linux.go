package continuity

import "golang.org/x/sys/unix"

func getxattr(path string, attr string, dest []byte) (sz int, err error) {
	return unix.Getxattr(path, attr, dest)
}

func listxattr(path string, dest []byte, flags int) (sz int, err error) {
	return unix.Listxattr(path, dest, flags)
}

func removexattr(path string, attr string) (err error) {
	return unix.Removexattr(path, attr)
}

func setxattr(path string, attr string, data []byte, flags int) (err error) {
	return unix.Setxattr(path, attr, data, flags)
}
