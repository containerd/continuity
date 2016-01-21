// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Derived from os/user package in Go standard library

package group

// Lookup looks up a group by groupname. If the group cannot be found, the
// returned error is of type UnknownGroupError.
func Lookup(groupname string) (*Group, error) {
	return lookup(groupname)
}

// LookupId looks up a group by groupid. If the group cannot be found, the
// returned error is of type UnknownGroupIdError.
func LookupId(gid string) (*Group, error) {
	return lookupId(gid)
}
