package config

import "os"

// configBase is %AppData% on Windows, which is where a program's settings
// belong there — a dotted directory in the home folder would be a Unix habit
// carried somewhere it does not read as configuration.
func configBase() (string, error) { return os.UserConfigDir() }
