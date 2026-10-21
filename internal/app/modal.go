package app

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/app/caps"
	"github.com/kiruva/tyr/internal/app/keymap"
)

// Every mode the app can be in is a modal: a dialog, an overlay, a full-screen
// tool, or the two panes themselves. What the root model has to know about one
// — where its keys go, how it is drawn, which text field the cursor blink
// belongs to, what the mouse does over it — is gathered here, in one entry per
// mode, rather than spread over a switch in Update, one in View and one in the
// mouse handler that all have to be kept in step by hand.
//
// The state a modal works on stays in Model. Most of them act on the panes, the
// running job or each other when they finish, so their handlers are Model
// methods and an entry here only points at them.
type modal struct {
	// key handles a keypress. Every mode has one.
	key func(Model, tea.KeyMsg) (tea.Model, tea.Cmd)

	// view draws the modal; nil leaves the panes showing. It is put over the
	// panes in a box unless fullScreen says it takes the whole terminal.
	view       func(Model) string
	fullScreen bool

	// focused is the text field taking input right now, which is where
	// non-key messages (the cursor blink) go; it may return nil. tick replaces
	// it for a modal whose blinking part is not a plain text field.
	focused func(*Model) *textinput.Model
	tick    func(*Model, tea.Msg) tea.Cmd

	// mouse handles the pointer. Without it, the wheel moves the list the
	// modal shows by wheel's step, and clicks are ignored: a dialog is a box
	// drawn at a size the mouse cannot be sure of, and a misplaced click in
	// one would act on the wrong row.
	mouse func(Model, tea.MouseMsg) (tea.Model, tea.Cmd)
	wheel func(*Model, int)
}

// modal returns the entry for the mode the app is in.
func (m Model) modal() modal {
	switch m.mode {
	case modeAddress:
		return modal{key: Model.onAddressKey,
			tick: func(m *Model, msg tea.Msg) tea.Cmd { return m.panes[m.active].UpdateAddr(msg) }}
	case modeConfirm:
		return modal{key: Model.onConfirmKey, view: Model.renderConfirm}
	case modeProgress:
		return modal{key: Model.onProgressKey, view: Model.renderProgress}
	case modeView:
		return modal{key: Model.onViewKey, view: Model.renderViewer, fullScreen: true,
			tick: (*Model).tickViewer, mouse: Model.viewerMouse}
	case modeEdit:
		// The editor has no timers of its own, so no tick.
		return modal{key: Model.onEditKey, view: Model.renderEditor, fullScreen: true, mouse: Model.editorMouse}
	case modeHelp:
		return modal{key: Model.onHelpKey, wheel: (*Model).scrollOverlayBy,
			view: func(m Model) string { return keymap.Help(m.keys, m.width, m.overlayRows(), m.overlayScroll) }}
	case modeCaps:
		return modal{key: Model.onCapsKey, wheel: (*Model).scrollOverlayBy,
			view: func(m Model) string { return caps.View(m.width, m.overlayRows(), m.overlayScroll) }}
	case modeTheme:
		return modal{key: Model.onThemeKey,
			view: func(m Model) string { return m.theme.View() },
			// The picker previews as it moves, which MoveTo does.
			wheel: func(m *Model, step int) { m.theme = m.theme.MoveTo(m.theme.Cursor() + step) }}
	case modeConn:
		return modal{key: Model.onConnKey, view: Model.renderConn, tick: (*Model).updateConnInputs}
	case modeCreate:
		return modal{key: Model.onCreateKey, view: Model.renderCreate,
			focused: func(m *Model) *textinput.Model { return &m.create.input }}
	case modeRenameOne:
		return modal{key: Model.onRenameOneKey, view: Model.renderRenameOne,
			focused: func(m *Model) *textinput.Model { return &m.renOne.input }}
	case modeRename:
		return modal{key: Model.onRenameKey, view: Model.renderRename, fullScreen: true,
			focused: func(m *Model) *textinput.Model { return &m.ren.fields[m.ren.focus] }}
	case modePack:
		return modal{key: Model.onPackKey, view: Model.renderPack, focused: (*Model).packFocused}
	case modeUnpackPw:
		return modal{key: Model.onUnpackPwKey, view: Model.renderUnpackPw,
			focused: func(m *Model) *textinput.Model { return &m.unpackPw.input }}
	case modeConvert:
		return modal{key: Model.onConvertKey, view: Model.renderConvert}
	case modeRotate:
		return modal{key: Model.onRotateKey, view: Model.renderRotate}
	case modeFilter:
		// Drawn in the pane's own address bar, so nothing over the panes.
		return modal{key: Model.onFilterKey,
			focused: func(m *Model) *textinput.Model { return &m.filter.input }}
	case modeFind:
		return modal{key: Model.onFindKey, view: Model.renderFind, focused: (*Model).findFocused,
			wheel: func(m *Model, step int) {
				if m.find.stage == findStageResults {
					m.find.cursor = clampIndex(m.find.cursor+step, len(m.find.results))
				}
			}}
	case modeSelectMask:
		return modal{key: Model.onSelectMaskKey, view: Model.renderSelectMask,
			focused: func(m *Model) *textinput.Model { return &m.selMask.input }}
	case modeConflict:
		return modal{key: Model.onConflictKey, view: Model.renderConflict}
	case modeProps:
		return modal{key: Model.onPropsKey, view: Model.renderProps,
			focused: func(m *Model) *textinput.Model { return &m.props.mode }}
	case modeSync:
		return modal{key: Model.onSyncKey, view: Model.renderSync, fullScreen: true,
			wheel: func(m *Model, step int) {
				if m.sync.stage == syncStageList {
					m.sync.cursor = clampIndex(m.sync.cursor+step, len(m.visibleSyncRows()))
				}
			}}
	case modeBookmarks:
		return modal{key: Model.onBookmarksKey, view: Model.renderBookmarks,
			wheel: func(m *Model, step int) {
				m.bookmarks.cursor = clampIndex(m.bookmarks.cursor+step, len(m.bookmarks.marks))
			}}
	case modeBookmarkAdd:
		return modal{key: Model.onBookmarkAddKey, view: Model.renderBookmarkAdd,
			focused: func(m *Model) *textinput.Model { return &m.bookmarkAdd.input }}
	case modeCommand:
		return modal{key: Model.onCommandKey, view: Model.renderCommand,
			focused: func(m *Model) *textinput.Model { return &m.command.input }}
	case modeRunning:
		return modal{key: Model.onRunningKey, view: Model.renderRunning}
	case modeKeys:
		return modal{key: Model.onKeyEditKey, fullScreen: true,
			view:  func(m Model) string { return m.keyEdit.View(m.keys, m.width, m.height) },
			wheel: func(m *Model, step int) { m.keyEdit = m.keyEdit.MoveTo(m.keyEdit.Cursor() + step) }}
	default:
		return modal{key: Model.onNormalKey, mouse: Model.paneMouse}
	}
}

// tick hands a non-key message to whatever in the current modal is waiting
// for one.
func (m Model) tick(msg tea.Msg) (tea.Model, tea.Cmd) {
	md := m.modal()
	if md.tick != nil {
		cmd := md.tick(&m, msg)
		return m, cmd
	}
	if md.focused == nil {
		return m, nil
	}
	f := md.focused(&m)
	if f == nil {
		return m, nil
	}
	var cmd tea.Cmd
	*f, cmd = f.Update(msg)
	return m, cmd
}
