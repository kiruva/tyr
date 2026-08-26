package remote

import "strings"

// looksLocal reports whether a target is plainly a path on this machine rather
// than a host. Windows paths collide with scp syntax two ways: a drive letter
// ("C:\Users", "d:/src") puts a colon where scp expects one, and a UNC share
// ("\\server\share") starts with separators Parse does not otherwise see.
func looksLocal(s string) bool {
	if strings.HasPrefix(s, `\`) {
		return true
	}
	if len(s) >= 2 && s[1] == ':' && isDriveLetter(s[0]) {
		return true
	}
	return false
}

func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
