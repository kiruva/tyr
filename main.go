package main

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/app"
	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/ui"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	args := os.Args[1:]

	var themeFlag, cdFile string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			fmt.Print(usage)
			return
		case arg == "--cd-file":
			if i+1 >= len(args) {
				fail("--cd-file needs a path")
			}
			i++
			cdFile = args[i]
		case strings.HasPrefix(arg, "--cd-file="):
			cdFile = strings.TrimPrefix(arg, "--cd-file=")
		case arg == "--version" || arg == "-v":
			fmt.Println("tyr", version)
			return
		case arg == "--themes":
			fmt.Println(strings.Join(ui.ThemeNames(), "\n"))
			return
		case arg == "--theme":
			if i+1 >= len(args) {
				fail("--theme needs a name (one of: " + ui.ThemeList() + ")")
			}
			i++
			themeFlag = args[i]
		case strings.HasPrefix(arg, "--theme="):
			themeFlag = strings.TrimPrefix(arg, "--theme=")
		}
	}

	notice := migrateConfig()

	// A config that cannot be read is not fatal: the app starts on defaults.
	cfg, _ := config.Load()

	if err := applyTheme(themeFlag, cfg); err != nil {
		fail(err.Error())
	}

	if cdFile == "" {
		cdFile = os.Getenv(app.CDFileEnv)
	}

	p := tea.NewProgram(app.New().WithSession(cfg).WithNotice(notice), tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tyr:", err)
		os.Exit(1)
	}

	// The shell wrapper reads this to follow tyr to wherever it ended up; a
	// child process cannot change its parent's directory any other way.
	if m, ok := final.(app.Model); ok {
		if err := app.WriteCDFile(cdFile, m.ActiveDir()); err != nil {
			fmt.Fprintln(os.Stderr, "tyr: could not write", cdFile+":", err)
		}
	}
}

// usage is what --help prints.
const usage = `tyr — a TUI file manager to rule them all

usage: tyr [options]

  --theme NAME     start with a colour scheme
  --themes         list the built-in themes
  --cd-file PATH   write the directory tyr exits in to PATH, for a shell
                   wrapper to cd into (also read from $TYR_CD_FILE)
  --version        print the version
  --help           print this

press ? inside tyr for the keys.
`

// migrateConfig moves a config directory left behind by the rename from
// lazyfiles, and returns what to tell the user about it. It runs before the
// theme is resolved so a migrated theme takes effect on this very first run.
// Failing to migrate is never fatal: the app starts on defaults, and the old
// directory is still sitting there for the user to move by hand.
func migrateConfig() string {
	mig, err := config.Migrate()
	switch {
	case err != nil:
		return "could not move the old config directory: " + err.Error()
	case mig != nil:
		return "config moved from " + mig.From + " to " + mig.To
	}
	return ""
}

// applyTheme resolves the theme from --theme, then $TYR_THEME, then the
// config file, and falls back to the built-in default. An unknown name is an
// error only when the user asked for it explicitly; a stale config file is
// ignored so a typo there can't stop the app from starting.
func applyTheme(flag string, cfg config.Config) error {
	for _, name := range []string{flag, os.Getenv("TYR_THEME")} {
		if name == "" {
			continue
		}
		t, ok := ui.ThemeByName(name)
		if !ok {
			return fmt.Errorf("unknown theme %q (one of: %s)", name, ui.ThemeList())
		}
		ui.Apply(t)
		return nil
	}

	if t, ok := ui.ThemeByName(cfg.Theme); ok {
		ui.Apply(t)
	}
	return nil
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "tyr:", msg)
	os.Exit(1)
}
