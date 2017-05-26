package conterrors

// Error codes common to the continuity package.

import "fmt"

var (
	ErrNotFound     = fmt.Errorf("not found")
	ErrNotSupported = fmt.Errorf("not supported")
)
