// Copyright 2011 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Derived from os/user package in Go standard library

// Package group allows group lookups by name or id.
package group

import "strconv"

// Group represents a group.
//
// On posix systems Gid contain a decimal number
// representing gid. On windows Gid
// contain security identifier (SID) in a string format.
type Group struct {
	Gid       string
	Groupname string
	// TODO(dmcgowan): Add support for memberlist
}

// UnknownGroupIdError is returned by LookupId when
// a group cannot be found.
type UnknownGroupIdError int

func (e UnknownGroupIdError) Error() string {
	return "group: unknown groupid " + strconv.Itoa(int(e))
}

// UnknownGroupError is returned by Lookup when
// a group cannot be found.
type UnknownGroupError string

func (e UnknownGroupError) Error() string {
	return "group: unknown group " + string(e)
}
