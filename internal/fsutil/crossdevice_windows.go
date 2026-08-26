package fsutil

import "syscall"

// errCrossDevice is ERROR_NOT_SAME_DEVICE. Windows does not report EXDEV, and
// syscall.EXDEV there is a plain errno constant that no API ever returns, so
// matching on it would silently never fire.
var errCrossDevice error = syscall.Errno(17)
