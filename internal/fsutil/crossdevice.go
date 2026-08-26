// Package fsutil holds the filesystem details that differ between platforms and
// that more than one package has to agree about.
package fsutil

import "errors"

// CrossDevice reports whether a failed os.Rename failed because the two paths
// are on different filesystems, which is the one rename error worth recovering
// from: the caller copies and removes instead.
func CrossDevice(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errCrossDevice)
}
