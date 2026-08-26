// Package app is the root Bubble Tea model wiring the two panes together.
package app

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/pane"
)

// mode is the top-level input mode / modal state.
type mode int

const (
	modeNormal      mode = iota // navigating the panes
	modeAddress                 // typing a path into the active pane's address bar
	modeConfirm                 // awaiting y/n on a pending operation
	modeProgress                // an operation is running
	modeView                    // read-only text pager
	modeEdit                    // nano-style text editor
	modeHelp                    // keybinding overlay
	modeCaps                    // capabilities overlay: what the tools on PATH allow
	modeTheme                   // theme picker overlay
	modeConn                    // ssh connection picker / form / password prompt
	modeCreate                  // naming a new file or directory
	modeRenameOne               // renaming the highlighted entry
	modeRename                  // the batch rename tool
	modePack                    // the pack dialog: format, level, password, name
	modeUnpackPw                // password prompt for an encrypted archive
	modeFilter                  // typing a filter that narrows the active pane
	modeFind                    // the find tool: query form, search, hit list
	modeSelectMask              // selecting or deselecting entries by mask
	modeConflict                // a destination exists: overwrite, skip, keep both?
	modeProps                   // file properties, with the permissions editable
	modeSync                    // the compare-and-synchronize tool
	modeBookmarks               // the bookmark list
	modeBookmarkAdd             // naming a new bookmark
	modeCommand                 // typing a shell command to run
	modeRunning                 // a shell command is running
	modeKeys                    // the key editor
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
	viewer     viewerState
	editor     textarea.Model
	edit       editTarget
	editOrig   string // content as loaded, for dirty detection
	editStatus string // transient footer message ("saved", etc.)

	// how far the help and capabilities overlays are scrolled, for a terminal
	// too short to hold either of them at once
	overlayScroll int

	// theme picker state
	themeCursor int    // highlighted row in the picker
	themeOrig   string // theme to restore if the picker is cancelled

	// ssh connection modal state
	conn connState

	// new file / new folder prompt state
	create createState

	// pack dialog, and the password prompt an encrypted archive triggers
	pack     packState
	unpackPw unpackPwState

	// rename state: the one-field prompt and the batch tool
	renOne renameOneState
	ren    renameState

	// what can be put back, most recent last, and whether the job running now
	// is one of those reversals
	undoStack []undoEntry
	undoing   bool

	// the collision question a copy or move is currently blocked on
	conflict conflictState

	// the properties dialog, with the permissions field in it
	props propsState

	// the compare-and-synchronize tool
	sync syncState

	// bookmarks: the list, and the prompt that adds one
	bookmarks   bookmarkState
	bookmarkAdd bookmarkAddState

	// the shell command prompt and the run it starts
	command commandState

	// the key editor
	keyEdit keyEditState

	// the last mouse click, for telling a double one from two singles
	lastClick clickRecord

	// deleteToTrash sends a delete to the desktop trash rather than unlinking
	deleteToTrash bool

	// narrowing the view: the filter prompt, the find tool, the mask prompt
	filter  filterState
	find    findState
	selMask selectMaskState
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
		panes:         [2]pane.Model{pane.New(wd), pane.New(wd)},
		active:        0,
		keys:          defaultKeys(),
		viewport:      viewport.New(0, 0),
		editor:        ta,
		deleteToTrash: true, // the config can turn it off; see WithSession
	}
}

// WithSession restores the view each pane was left in on the last run: its sort
// order and hidden-file setting always, and the directory it was in when the
// config asks for that. A directory that has since gone away is skipped, so a
// removed mount cannot stop the app from starting.
func (m Model) WithSession(cfg config.Config) Model {
	m.deleteToTrash = cfg.TrashDeletes()

	if len(cfg.Keys) > 0 {
		binds, unknown := fromConfigKeys(cfg.Keys)
		m.keys = keysFrom(binds)
		if len(unknown) > 0 {
			// A misspelled action would otherwise do nothing at all, quietly.
			m.noticeText = "config: no such key action: " + strings.Join(unknown, ", ")
		}
	}

	restore := cfg.RestorePaths()
	for i := range m.panes {
		saved := cfg.Panes[i]

		sort, ok := pane.ParseSort(saved.Sort)
		if !ok {
			sort = pane.SortName
		}
		m.panes[i].SetView(sort, saved.Hidden)

		if restore && saved.Path != "" {
			_ = m.panes[i].GoTo(saved.Path) // gone or unreadable: stay on the default
		}
	}
	return m
}

// saveSession records where the panes are and how they are showing things, for
// the next run to restore. It is best-effort: the app is on its way out, and
// there is nowhere left to show an error about a config file.
func (m Model) saveSession() {
	var state [2]config.PaneState
	for i := range m.panes {
		p := &m.panes[i]
		state[i] = config.PaneState{Sort: p.SortModeLabel(), Hidden: p.HiddenShown()}

		switch {
		case p.IsRemote():
			// The connection is not restored, so neither is the location.
		case p.InArchive():
			state[i].Path = filepath.Dir(p.ArchivePath())
		default:
			state[i].Path = p.Path
		}
	}
	_ = config.SaveSession(state)
}

// WithNotice seeds the status bar with a one-off message, shown until the first
// keypress. It is how startup work done before the program runs — a migrated
// config directory, say — gets in front of the user, since anything printed
// before the alt screen opens is not visible until after the app exits.
func (m Model) WithNotice(text string) Model {
	switch {
	case text == "":
	case m.noticeText == "":
		m.noticeText = text
	default:
		// Startup can have more than one thing to say; neither should win.
		m.noticeText += " · " + text
	}
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }
