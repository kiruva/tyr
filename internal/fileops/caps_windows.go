package fileops

// See caps_unix.go. On Windows `!` opens %COMSPEC% and a one-liner is run by
// cmd, which every install has — so this row is green unless something has
// gone badly wrong with PATH.
const (
	shellLabel  = "%COMSPEC%"
	shellBinary = "cmd"
)
