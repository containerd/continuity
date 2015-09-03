package continuity

import (
	"bytes"
	"syscall"
)

const (
	XATTR_NOFOLLOW = iota << 1
	XATTR_CREATE
	XATTR_REPLACE
)

const defaultXattrBufferSize = 5

func Setxattr(path, name string, data []byte) error {
	return setxattr(path, name, data, XATTR_NOFOLLOW)
}

func Listxattr(path string) ([]string, error) {
	var p []byte // nil on first execution

	for {
		n, err := listxattr(path, p, XATTR_NOFOLLOW) // first call gets buffer size.
		if err != nil {
			return nil, err
		}

		if n > len(p) {
			p = make([]byte, n)
			continue
		}

		p = p[:n]

		ps := bytes.Split(bytes.TrimSuffix(p, []byte{0}), []byte{0})
		var entries []string
		for _, p := range ps {
			s := string(p)
			if s != "" {
				entries = append(entries, s)
			}
		}

		return entries, nil
	}
}

func Getxattr(path, attr string) ([]byte, error) {
	var p []byte = make([]byte, defaultXattrBufferSize)
	for {
		n, err := getxattr(path, attr, p, XATTR_NOFOLLOW)
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERANGE {
				p = make([]byte, len(p)*2) // this can't be ideal.
				continue                   // try again!
			}

			return nil, err
		}

		// realloc to correct size and repeat
		if n > len(p) {
			p = make([]byte, n)
			continue
		}

		return p[:n], nil
	}
}
