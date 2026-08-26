package main

import (
	"fmt"
	"os"
	"path/filepath"
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

	// The config directory has to be settled before anything reads a theme out
	// of it, and a leftover directory from the rename is moved on the way.
	notice := migrateConfig()
	themeNotice := loadUserThemes()

	var themeFlag, cdFile, newTheme string
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
			if themeNotice != "" {
				fmt.Fprintln(os.Stderr, "tyr:", themeNotice)
			}
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
		case arg == "--new-theme":
			if i+1 >= len(args) {
				fail("--new-theme needs a name")
			}
			i++
			newTheme = args[i]
		case strings.HasPrefix(arg, "--new-theme="):
			newTheme = strings.TrimPrefix(arg, "--new-theme=")
		}
	}

	// A config that cannot be read is not fatal: the app starts on defaults.
	cfg, _ := config.Load()

	if newTheme != "" {
		writeThemeTemplate(newTheme, themeFlag, cfg)
		return
	}

	if err := applyTheme(themeFlag, cfg); err != nil {
		fail(err.Error())
	}

	if cdFile == "" {
		cdFile = os.Getenv(app.CDFileEnv)
	}

	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if cfg.MouseEnabled() {
		// Cell motion is clicks and the wheel, and leaves the terminal's own
		// selection alone while no button is held.
		opts = append(opts, tea.WithMouseCellMotion())
	}

	model := app.New().WithSession(cfg).WithNotice(themeNotice).WithNotice(notice)
	p := tea.NewProgram(model, opts...)
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
  --themes         list the themes, built-in and your own
  --new-theme NAME write a theme file to copy from, then exit
  --cd-file PATH   write the directory tyr exits in to PATH, for a shell
                   wrapper to cd into (also read from $TYR_CD_FILE)
  --version        print the version
  --help           print this

press ? inside tyr for the keys.
`

// loadUserThemes adds whatever is in <config>/themes to the picker, and returns
// what to say about any file it could not read. A broken theme file costs its
// own theme and nothing else.
func loadUserThemes() string {
	dir, err := config.Dir()
	if err != nil {
		return ""
	}
	_, problems := ui.LoadUserThemes(filepath.Join(dir, ui.ThemeDir))
	if len(problems) == 0 {
		return ""
	}
	return "theme " + strings.Join(problems, " · ")
}

// writeThemeTemplate answers --new-theme: it writes the current theme out as a
// file to edit, which is easier to start from than an empty one.
func writeThemeTemplate(name, themeFlag string, cfg config.Config) {
	if err := applyTheme(themeFlag, cfg); err != nil {
		fail(err.Error())
	}

	dir, err := config.Dir()
	if err != nil {
		fail(err.Error())
	}
	path := filepath.Join(dir, ui.ThemeDir, name+ui.ThemeExt)
	if _, err := os.Stat(path); err == nil {
		fail(path + " already exists")
	}
	if err := ui.WriteThemeTemplate(path); err != nil {
		fail(err.Error())
	}
	fmt.Println("wrote", path)
}

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
