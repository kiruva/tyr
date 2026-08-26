//go:build !windows

package fsutil

import "syscall"

// errCrossDevice is EXDEV, which every Unix returns for a rename across
// filesystems.
var errCrossDevice error = syscall.EXDEV
