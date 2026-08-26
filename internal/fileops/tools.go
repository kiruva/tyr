package fileops

import "os/exec"

// Some tools answer to more than one command name depending on how they were
// packaged. 7-Zip is the awkward one: p7zip installs `7z`, the official
// distribution installs `7zz`, and the reduced build in older packages is `7za`
// (enough for .7z and .zip, which is all tyr asks of it). Everywhere else in
// this package a tool is named by its canonical name — "7z" — and resolved to
// whichever variant is actually installed just before it runs.
var alternates = map[string][]string{
	"7z": {"7z", "7zz", "7za"},
}

// onPath reports whether a tool is installed, under its own name or any of the
// names it also ships as.
func onPath(bin string) bool { return resolveTool(bin, installed) != "" }

// resolveTool returns the command name to run for a canonical tool name, or ""
// when none of its variants is installed. have is the PATH lookup, injected so
// this is testable without touching the machine's PATH.
func resolveTool(bin string, have func(string) bool) string {
	names, ok := alternates[bin]
	if !ok {
		names = []string{bin}
	}
	for _, n := range names {
		if have(n) {
			return n
		}
	}
	return ""
}

// tool is resolveTool against the real PATH: the command to exec, or "".
func tool(bin string) string { return resolveTool(bin, installed) }

// installed is the real PATH lookup for one exact command name.
func installed(bin string) bool {
	_, err := exec.LookPath(bin)
	return err == nil
}
