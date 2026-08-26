package app

import "os"

// ownership has nothing to report on Windows: a file's security descriptor is
// an ACL rather than a uid/gid pair, and a link count is not part of what
// os.Stat returns there. The dialog leaves the owner row out when the name
// comes back empty, so this is a blank rather than a wrong answer.
func ownership(os.FileInfo) (owner, group string, links uint64) {
	return "", "", 0
}
