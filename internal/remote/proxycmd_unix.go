//go:build !windows

package remote

// proxyShell runs a ProxyCommand the way ssh does: through /bin/sh, with
// "exec" in front, so the command replaces the shell and stopping it stops
// the command rather than a shell that would leave it running.
func proxyShell(command string) (string, []string) {
	return "/bin/sh", []string{"-c", "exec " + command}
}
