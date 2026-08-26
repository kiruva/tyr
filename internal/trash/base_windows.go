package trash

import (
	"os"
	"path/filepath"
)

// dataBase puts the trash under %LocalAppData%\tyr, so the layout is
// %LocalAppData%\tyr\Trash\{files,info}.
//
// This is tyr's own trash and not the Windows Recycle Bin: the Bin is reached
// through a shell API that keeps its own records, and a file dropped in there
// could not be put back by Ctrl+Z. A directory tyr owns keeps delete reversible
// on the terms the rest of the app promises — the cost is that Explorer's
// "Restore" knows nothing about it.
func dataBase() (string, error) {
	if base := os.Getenv("LOCALAPPDATA"); base != "" {
		return filepath.Join(base, "tyr"), nil
	}
	base, err := os.UserCacheDir() // %LocalAppData% by another name
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "tyr"), nil
}
