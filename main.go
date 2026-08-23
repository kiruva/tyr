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

	var themeFlag string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
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

	if err := applyTheme(themeFlag); err != nil {
		fail(err.Error())
	}

	p := tea.NewProgram(app.New().WithNotice(notice), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tyr:", err)
		os.Exit(1)
	}
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
func applyTheme(flag string) error {
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

	cfg, err := config.Load()
	if err != nil {
		return nil // unreadable config shouldn't block startup
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
