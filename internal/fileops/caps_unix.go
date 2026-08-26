//go:build !windows

package fileops

// The shell the capabilities overlay names is the one internal/app actually
// runs: $SHELL for `!`, sh for a one-liner.
const (
	shellLabel  = "$SHELL"
	shellBinary = "sh"
)
