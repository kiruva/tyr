//go:build !windows

package remote

// looksLocal reports whether a target is plainly a path on this machine rather
// than a host. On Unix the prefixes Parse already checks cover it: nothing else
// with a colon in it is a local path, and "srv:" is a perfectly good hostname.
func looksLocal(string) bool { return false }
