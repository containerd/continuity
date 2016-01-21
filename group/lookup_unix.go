// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Derived from os/user package in Go standard library

// +build darwin dragonfly freebsd !android,linux netbsd openbsd solaris
// +build cgo

package group

import (
	"fmt"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"
)

/*
#cgo solaris CFLAGS: -D_POSIX_PTHREAD_SEMANTICS
#include <unistd.h>
#include <sys/types.h>
#include <grp.h>
#include <stdlib.h>

static int mygetgrgid_r(int uid, struct group *grp,
	char *buf, size_t buflen, struct group **result) {
	return getgrgid_r(uid, grp, buf, buflen, result);
}

static int mygetgrnam_r(const char *name, struct group *grp,
	char *buf, size_t buflen, struct group **result) {
	return getgrnam_r(name, grp, buf, buflen, result);
}
*/
import "C"

func lookup(group string) (*Group, error) {
	return lookupUnix(-1, group, true)
}

func lookupId(gid string) (*Group, error) {
	i, e := strconv.Atoi(gid)
	if e != nil {
		return nil, e
	}
	return lookupUnix(i, "", false)
}

func lookupUnix(gid int, name string, lookupByName bool) (*Group, error) {
	var grp C.struct_group
	var result *C.struct_group

	var bufSize C.long
	if runtime.GOOS == "dragonfly" || runtime.GOOS == "freebsd" {
		// DragonFly and FreeBSD do not have _SC_GETPW_R_SIZE_MAX
		// and just return -1.  So just use the same
		// size that Linux returns.
		bufSize = 1024
	} else {
		bufSize = C.sysconf(C._SC_GETGR_R_SIZE_MAX)
		if bufSize <= 0 || bufSize > 1<<20 {
			return nil, fmt.Errorf("group: unreasonable _SC_GETGR_R_SIZE_MAX of %d", bufSize)
		}
	}
	buf := C.malloc(C.size_t(bufSize))
	defer C.free(buf)
	var rv C.int
	if lookupByName {
		nameC := C.CString(name)
		defer C.free(unsafe.Pointer(nameC))
		// mygetpwnam_r is a wrapper around getpwnam_r to avoid
		// passing a size_t to getpwnam_r, because for unknown
		// reasons passing a size_t to getpwnam_r doesn't work on
		// Solaris.
		rv = C.mygetgrnam_r(nameC,
			&grp,
			(*C.char)(buf),
			C.size_t(bufSize),
			&result)
		if rv != 0 {
			return nil, fmt.Errorf("group: lookup group %s: %s", name, syscall.Errno(rv))
		}
		if result == nil {
			return nil, UnknownGroupError(name)
		}
	} else {
		// mygetpwuid_r is a wrapper around getpwuid_r to
		// to avoid using uid_t because C.uid_t(uid) for
		// unknown reasons doesn't work on linux.
		rv = C.mygetgrgid_r(C.int(gid),
			&grp,
			(*C.char)(buf),
			C.size_t(bufSize),
			&result)
		if rv != 0 {
			return nil, fmt.Errorf("group: lookup groupid %d: %s", gid, syscall.Errno(rv))
		}
		if result == nil {
			return nil, UnknownGroupIdError(gid)
		}
	}

	g := &Group{
		Groupname: C.GoString(grp.gr_name),
		Gid:       strconv.Itoa(int(grp.gr_gid)),
		// TODO(dmcgowan): Support member list "char **gr_mem"
	}
	return g, nil
}
