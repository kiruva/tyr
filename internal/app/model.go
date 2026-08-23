// Package app is the root Bubble Tea model wiring the two panes together.
package app

import (
	"os"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/pane"
)

// mode is the top-level input mode / modal state.
type mode int

const (
	modeNormal   mode = iota // navigating the panes
	modeAddress              // typing a path into the active pane's address bar
	modeConfirm              // awaiting y/n on a pending operation
	modeProgress             // an operation is running
	modeView                 // read-only text pager
	modeEdit                 // nano-style text editor
	modeHelp                 // keybinding overlay
	modeTheme                // theme picker overlay
	modeConn                 // ssh connection picker / form / password prompt
	modeCreate               // naming a new file or directory
)

// editTarget records what an edit session is writing back to.
type editTarget struct {
	realPath string // set for a real file on disk
	archive  string // set for an archive member
	member   string // in-archive path (with archive)
	title    string // display name
}

// Model is the top-level application state.
type Model struct {
	panes  [2]pane.Model
	active int // 0 = left, 1 = right
	width  int
	height int
	keys   keyMap

	mode mode

	// operation state
	pending       fileops.Job // job awaiting confirmation
	willOverwrite bool        // pending job would clobber existing files
	progress      fileops.Progress
	progressCh    <-chan any
	errText       string
	noticeText    string // non-error banner, cleared by the next keypress

	// view/edit state
	viewport   viewport.Model
	viewTitle  string
	editor     textarea.Model
	edit       editTarget
	editOrig   string // content as loaded, for dirty detection
	editStatus string // transient footer message ("saved", etc.)

	// theme picker state
	themeCursor int    // highlighted row in the picker
	themeOrig   string // theme to restore if the picker is cancelled

	// ssh connection modal state
	conn connState

	// new file / new folder prompt state
	create createState
}

// New constructs the app with both panes rooted at the current directory.
func New() Model {
	wd, err := os.Getwd()
	if err != nil {
		wd = string(os.PathSeparator)
	}

	ta := textarea.New()
	ta.CharLimit = 0 // no limit
	ta.ShowLineNumbers = true
	ta.Prompt = ""

	return Model{
		panes:    [2]pane.Model{pane.New(wd), pane.New(wd)},
		active:   0,
		keys:     defaultKeys(),
		viewport: viewport.New(0, 0),
		editor:   ta,
	}
}

// WithNotice seeds the status bar with a one-off message, shown until the first
// keypress. It is how startup work done before the program runs — a migrated
// config directory, say — gets in front of the user, since anything printed
// before the alt screen opens is not visible until after the app exits.
func (m Model) WithNotice(text string) Model {
	m.noticeText = text
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }
