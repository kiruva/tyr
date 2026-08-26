package remote

import (
	"context"
	"path"
)

// Mkdir creates a directory on the far side, including missing parents. It
// fails if the name is already taken — `mkdir -p` alone would succeed silently.
func Mkdir(h Host, p string) error {
	return run(context.Background(), h, refuseExisting(p)+"mkdir -p -- "+shQuote(p))
}

// Touch creates an empty file on the far side, including missing parents, and
// fails if the name is already taken.
func Touch(h Host, p string) error {
	return run(context.Background(), h, refuseExisting(p)+
		"mkdir -p -- "+shQuote(path.Dir(p))+" && : > "+shQuote(p))
}

// refuseExisting is a script prefix that exits non-zero when p already exists,
// so the shell's own message ("already exists") reaches the status bar.
func refuseExisting(p string) string {
	return "if [ -e " + shQuote(p) + " ]; then echo 'already exists' >&2; exit 1; fi; "
}
