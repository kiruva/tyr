//go:build windows

package remote

// proxyShell runs a ProxyCommand through cmd.exe, which is where Windows'
// own ssh client runs one too.
func proxyShell(command string) (string, []string) {
	return "cmd.exe", []string{"/c", command}
}
