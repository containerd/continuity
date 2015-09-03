package continuity

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// These functions were generated using golang.org/x/sys/unix package. They can be removed once the sys package gets xattr
// support on darwin. First, the following diff was applied to syscall_darwin.go:
//
// diff --git a/unix/syscall_darwin.go b/unix/syscall_darwin.go
// index 0d1771c..dd2f843 100644
// --- a/unix/syscall_darwin.go
// +++ b/unix/syscall_darwin.go
// @@ -352,6 +352,10 @@ func Kill(pid int, signum syscall.Signal) (err error) { return kill(pid, int(sig
//  // Waitevent
//  // Modwatch
//  // Getxattr
// +//sys  Getxattr(path string, attr string, dest []byte) (sz int, err error)
// +//sys  Listxattr(path string, dest []byte, flags int) (sz int, err error)
// +//sys  Removexattr(path string, attr string) (err error)
// +//sys  Setxattr(path string, attr string, data []byte, flags int) (err error)
//  // Fgetxattr
//  // Setxattr
//  // Fsetxattr
//
// The following command was run to generate the extra syscalls:
//
// 	$ GOOS=darwin GOARCH=amd64 ./mksyscall.pl syscall_bsd.go syscall_darwin.go syscall_darwin_amd64.go > zsyscall_darwin_amd64.go
//
// Once generated, these functions were manually dropped into this file.

func getxattr(path string, attr string, dest []byte, flags int) (sz int, err error) {
	var _p0 *byte
	_p0, err = unix.BytePtrFromString(path)
	if err != nil {
		return
	}
	var _p1 *byte
	_p1, err = unix.BytePtrFromString(attr)
	if err != nil {
		return
	}
	var _p2 unsafe.Pointer
	if len(dest) > 0 {
		_p2 = unsafe.Pointer(&dest[0])
	} else {
		_p2 = unsafe.Pointer(&_zero)
	}

	// NOTE(stevvooe): Done a little hacking here but I'm sure we are not
	// getting this syscall correct on OS X. We are still following links.
	r0, _, e1 := unix.Syscall6(unix.SYS_GETXATTR, uintptr(unsafe.Pointer(_p0)), uintptr(unsafe.Pointer(_p1)), uintptr(_p2), uintptr(len(dest)), 0, uintptr(flags))
	use(unsafe.Pointer(_p0))
	use(unsafe.Pointer(_p1))
	sz = int(r0)
	if e1 != 0 {
		err = syscall.Errno(e1)
	}
	return
}

func listxattr(path string, dest []byte, flags int) (sz int, err error) {
	var _p0 *byte
	_p0, err = unix.BytePtrFromString(path)
	if err != nil {
		return
	}
	var _p1 unsafe.Pointer
	if len(dest) > 0 {
		_p1 = unsafe.Pointer(&dest[0])
	} else {
		_p1 = unsafe.Pointer(&_zero)
	}
	r0, _, e1 := unix.Syscall6(unix.SYS_LISTXATTR, uintptr(unsafe.Pointer(_p0)), uintptr(_p1), uintptr(len(dest)), uintptr(flags), 0, 0)
	use(unsafe.Pointer(_p0))
	sz = int(r0)
	if e1 != 0 {
		err = syscall.Errno(e1)
	}
	return
}

func removexattr(path string, attr string) (err error) {
	var _p0 *byte
	_p0, err = unix.BytePtrFromString(path)
	if err != nil {
		return
	}
	var _p1 *byte
	_p1, err = unix.BytePtrFromString(attr)
	if err != nil {
		return
	}
	_, _, e1 := unix.Syscall(unix.SYS_REMOVEXATTR, uintptr(unsafe.Pointer(_p0)), uintptr(unsafe.Pointer(_p1)), 0)
	use(unsafe.Pointer(_p0))
	use(unsafe.Pointer(_p1))
	if e1 != 0 {
		err = syscall.Errno(e1)
	}
	return
}

func setxattr(path string, attr string, data []byte, flags int) (err error) {
	var _p0 *byte
	_p0, err = unix.BytePtrFromString(path)
	if err != nil {
		return
	}
	var _p1 *byte
	_p1, err = unix.BytePtrFromString(attr)
	if err != nil {
		return
	}
	var _p2 unsafe.Pointer
	if len(data) > 0 {
		_p2 = unsafe.Pointer(&data[0])
	} else {
		_p2 = unsafe.Pointer(&_zero)
	}
	_, _, e1 := unix.Syscall6(unix.SYS_SETXATTR, uintptr(unsafe.Pointer(_p0)), uintptr(unsafe.Pointer(_p1)), uintptr(_p2), uintptr(len(data)), uintptr(flags), 0)
	use(unsafe.Pointer(_p0))
	use(unsafe.Pointer(_p1))
	if e1 != 0 {
		err = syscall.Errno(e1)
	}
	return
}

// redefinitions from sys/unix below here.

var _zero uintptr

// use is a no-op, but the compiler cannot see that it is.
// Calling use(p) ensures that p is kept live until that point.
//go:noescape
func use(p unsafe.Pointer)
