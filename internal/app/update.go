package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/remote"
	"github.com/kiruva/tyr/internal/rename"
	"github.com/kiruva/tyr/internal/trash"
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizePanes()
		return m, nil

	case fileops.Progress:
		m.progress = msg
		return m, waitCmd(m.progressCh)

	case fileops.Conflict:
		return m.onConflict(msg)

	case fileops.Result:
		return m.finishOp(msg)

	case remoteListMsg:
		return m.onRemoteList(msg)

	case connResultMsg:
		return m.onConnResult(msg)

	case createdMsg:
		return m.onCreated(msg)

	case renameCollectedMsg:
		return m.onRenameCollected(msg)

	case dirSizeMsg:
		return m.onDirSize(msg)

	case findDoneMsg:
		return m.onFindDone(msg)

	case syncDoneMsg:
		return m.onSyncDone(msg)

	case shellDoneMsg:
		return m.onShellDone(msg)

	case commandDoneMsg:
		return m.onCommandDone(msg)

	case tea.MouseMsg:
		return m.onMouse(msg)

	case tea.KeyMsg:
		return m.onKey(msg)
	}

	// Non-key messages (e.g. cursor blink) are routed to the active component.
	switch m.mode {
	case modeAddress:
		cmd := m.panes[m.active].UpdateAddr(msg)
		return m, cmd
	case modeConn:
		cmd := m.updateConnInputs(msg)
		return m, cmd
	case modeCreate:
		var cmd tea.Cmd
		m.create.input, cmd = m.create.input.Update(msg)
		return m, cmd
	case modeRenameOne:
		var cmd tea.Cmd
		m.renOne.input, cmd = m.renOne.input.Update(msg)
		return m, cmd
	case modeRename:
		var cmd tea.Cmd
		m.ren.fields[m.ren.focus], cmd = m.ren.fields[m.ren.focus].Update(msg)
		return m, cmd
	case modePack:
		var cmd tea.Cmd
		switch m.pack.focus {
		case packFieldPassword:
			m.pack.pw, cmd = m.pack.pw.Update(msg)
		case packFieldName:
			m.pack.name, cmd = m.pack.name.Update(msg)
		}
		return m, cmd
	case modeUnpackPw:
		var cmd tea.Cmd
		m.unpackPw.input, cmd = m.unpackPw.input.Update(msg)
		return m, cmd
	case modeFilter:
		var cmd tea.Cmd
		m.filter.input, cmd = m.filter.input.Update(msg)
		return m, cmd
	case modeProps:
		var cmd tea.Cmd
		m.props.mode, cmd = m.props.mode.Update(msg)
		return m, cmd
	case modeBookmarkAdd:
		var cmd tea.Cmd
		m.bookmarkAdd.input, cmd = m.bookmarkAdd.input.Update(msg)
		return m, cmd
	case modeCommand:
		var cmd tea.Cmd
		m.command.input, cmd = m.command.input.Update(msg)
		return m, cmd
	case modeSelectMask:
		var cmd tea.Cmd
		m.selMask.input, cmd = m.selMask.input.Update(msg)
		return m, cmd
	case modeFind:
		var cmd tea.Cmd
		switch m.find.focus {
		case findFieldName:
			m.find.name, cmd = m.find.name.Update(msg)
		case findFieldContent:
			m.find.content, cmd = m.find.content.Update(msg)
		}
		return m, cmd
	case modeEdit:
		var cmd tea.Cmd
		m.editor, cmd = m.editor.Update(msg)
		return m, cmd
	case modeView:
		var cmd tea.Cmd
		if m.viewer.prompting {
			m.viewer.input, cmd = m.viewer.input.Update(msg)
			return m, cmd
		}
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// onKey dispatches a keypress to the handler for the current mode.
func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.noticeText = "" // a startup notice is read once and dismissed by any key

	switch m.mode {
	case modeAddress:
		return m.onAddressKey(msg)
	case modeConfirm:
		return m.onConfirmKey(msg)
	case modeProgress:
		return m.onProgressKey(msg)
	case modeView:
		return m.onViewKey(msg)
	case modeEdit:
		return m.onEditKey(msg)
	case modeHelp:
		// The two overlays swap into each other; anything else dismisses.
		if key.Matches(msg, m.keys.Caps) {
			m.mode, m.overlayScroll = modeCaps, 0
			return m, nil
		}
		return m.scrollOverlay(msg)
	case modeCaps:
		if key.Matches(msg, m.keys.Help) {
			m.mode, m.overlayScroll = modeHelp, 0
			return m, nil
		}
		return m.scrollOverlay(msg)
	case modeTheme:
		return m.onThemeKey(msg)
	case modeConn:
		return m.onConnKey(msg)
	case modeCreate:
		return m.onCreateKey(msg)
	case modeRenameOne:
		return m.onRenameOneKey(msg)
	case modeRename:
		return m.onRenameKey(msg)
	case modePack:
		return m.onPackKey(msg)
	case modeUnpackPw:
		return m.onUnpackPwKey(msg)
	case modeFilter:
		return m.onFilterKey(msg)
	case modeConflict:
		return m.onConflictKey(msg)
	case modeProps:
		return m.onPropsKey(msg)
	case modeSync:
		return m.onSyncKey(msg)
	case modeBookmarks:
		return m.onBookmarksKey(msg)
	case modeBookmarkAdd:
		return m.onBookmarkAddKey(msg)
	case modeCommand:
		return m.onCommandKey(msg)
	case modeRunning:
		return m.onRunningKey(msg)
	case modeKeys:
		return m.onKeyEditKey(msg)
	case modeSelectMask:
		return m.onSelectMaskKey(msg)
	case modeFind:
		return m.onFindKey(msg)
	default:
		return m.onNormalKey(msg)
	}
}

// scrollOverlay moves through an overlay too tall for the terminal. Anything
// that is not a scroll key closes it, which is what "any key to close" means.
func (m Model) scrollOverlay(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	step := m.overlayRows() - 1

	switch msg.String() {
	case "up", "k":
		m.overlayScroll = max(m.overlayScroll-1, 0)
	case "down", "j":
		m.overlayScroll++
	case "pgup", "ctrl+u":
		m.overlayScroll = max(m.overlayScroll-step, 0)
	case "pgdown", "ctrl+d":
		m.overlayScroll += step
	case "home", "g":
		m.overlayScroll = 0
	default:
		m.mode, m.overlayScroll = modeNormal, 0
	}
	return m, nil
}

// onNormalKey handles navigation and triggers operations / view / edit.
func (m Model) onNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.errText = "" // any key clears a stale error banner
	p := &m.panes[m.active]

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.saveSession()    // where the panes were, for the next run
		remote.ForgetAll() // close sessions and drop passwords from memory
		return m, tea.Quit
	case key.Matches(msg, m.keys.Up):
		p.MoveUp()
	case key.Matches(msg, m.keys.Down):
		p.MoveDown()
	case key.Matches(msg, m.keys.PageUp):
		p.PageUp()
	case key.Matches(msg, m.keys.PageDown):
		p.PageDown()
	case key.Matches(msg, m.keys.Top):
		p.Top()
	case key.Matches(msg, m.keys.Bottom):
		p.Bottom()
	case key.Matches(msg, m.keys.Enter):
		if p.IsRemote() {
			cmd := m.remoteEnter(m.active)
			return m, cmd
		}
		m.enterOrOpen()
	case key.Matches(msg, m.keys.Back):
		if p.IsRemote() {
			cmd := m.remoteAscend(m.active)
			return m, cmd
		}
		p.Ascend()
	case key.Matches(msg, m.keys.Address):
		m.mode = modeAddress
		cmd := p.BeginEditPath()
		return m, cmd
	case key.Matches(msg, m.keys.Select):
		// Space marks the entry; on a directory it also asks for the one thing
		// the listing cannot say — how much is inside it.
		cmd := m.sizeCurrentDir()
		p.ToggleSelect()
		return m, cmd
	case key.Matches(msg, m.keys.SelectAll):
		m.selectAll()
	case key.Matches(msg, m.keys.Invert):
		m.invertSelection()
	case key.Matches(msg, m.keys.SelectMask):
		cmd := m.openSelectMask(true)
		return m, cmd
	case key.Matches(msg, m.keys.Deselect):
		cmd := m.openSelectMask(false)
		return m, cmd
	case key.Matches(msg, m.keys.Bookmarks):
		cmd := m.openBookmarks()
		return m, cmd
	case key.Matches(msg, m.keys.BookmarkAdd):
		cmd := m.openBookmarkAdd()
		return m, cmd
	case key.Matches(msg, m.keys.Shell):
		cmd := m.openShell()
		return m, cmd
	case key.Matches(msg, m.keys.Command):
		cmd := m.openCommand()
		return m, cmd
	case key.Matches(msg, m.keys.Sync):
		cmd := m.openSync()
		return m, cmd
	case key.Matches(msg, m.keys.Props):
		cmd := m.openProps()
		return m, cmd
	case key.Matches(msg, m.keys.Filter):
		cmd := m.openFilter()
		return m, cmd
	case key.Matches(msg, m.keys.Find):
		cmd := m.openFind()
		return m, cmd
	case key.Matches(msg, m.keys.SizeDirs):
		cmd := m.sizeAllDirs()
		return m, cmd
	case key.Matches(msg, m.keys.Refresh):
		cmd := m.refreshPane(m.active)
		return m, cmd
	case msg.String() == "esc":
		// Nothing modal is open, so Esc is the way out of a narrowed pane.
		p.ClearFilter()
	case key.Matches(msg, m.keys.Hidden):
		p.ToggleHidden()
	case key.Matches(msg, m.keys.Sort):
		p.CycleSort()
	case key.Matches(msg, m.keys.Switch):
		m.active = 1 - m.active
	case key.Matches(msg, m.keys.Theme):
		m.openThemePicker()
	case key.Matches(msg, m.keys.Keys):
		m.openKeyEditor()
	case key.Matches(msg, m.keys.Connect):
		cmd := m.openConnPicker()
		return m, cmd
	case key.Matches(msg, m.keys.Help):
		m.mode, m.overlayScroll = modeHelp, 0
	case key.Matches(msg, m.keys.Caps):
		m.mode, m.overlayScroll = modeCaps, 0
	case key.Matches(msg, m.keys.View):
		cmd := m.openViewer()
		return m, cmd
	case key.Matches(msg, m.keys.Edit):
		cmd := m.openEditor()
		return m, cmd
	case key.Matches(msg, m.keys.NewFile):
		cmd := m.openCreate(false)
		return m, cmd
	case key.Matches(msg, m.keys.NewDir):
		cmd := m.openCreate(true)
		return m, cmd
	case key.Matches(msg, m.keys.Copy):
		m.beginOp(fileops.OpCopy)
	case key.Matches(msg, m.keys.Move):
		m.beginOp(fileops.OpMove)
	case key.Matches(msg, m.keys.Rename):
		cmd := m.openRenameOne()
		return m, cmd
	case key.Matches(msg, m.keys.RenameMulti):
		cmd := m.openRename()
		return m, cmd
	case key.Matches(msg, m.keys.Undo):
		m.openUndo()
	case key.Matches(msg, m.keys.Delete):
		m.beginOp(m.deleteOp())
	case key.Matches(msg, m.keys.DeletePerm):
		m.beginOp(fileops.OpDelete)
	case key.Matches(msg, m.keys.Pack):
		m.beginOp(fileops.OpPack)
	case key.Matches(msg, m.keys.Unpack):
		m.beginOp(fileops.OpUnpack)
	case key.Matches(msg, m.keys.Unwrap):
		m.beginOp(fileops.OpUnwrap)
	}
	return m, nil
}

// onAddressKey drives the pane's address bar. Enter jumps, Esc restores the
// previous location; a bad path keeps the bar open with the error in the
// status line so it can be corrected.
func (m Model) onAddressKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.panes[m.active]
	switch msg.String() {
	case "enter":
		// commitAddress sets the resulting mode itself: a local or same-host jump
		// returns to normal, a new remote target opens the connection modal.
		cmd, err := m.commitAddress()
		if err != nil {
			m.errText = err.Error()
			return m, nil
		}
		m.errText = ""
		return m, cmd
	case "esc":
		p.CancelEditPath()
		m.mode = modeNormal
		m.errText = ""
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	}
	m.errText = ""
	cmd := p.UpdateAddr(msg)
	return m, cmd
}

// onConfirmKey handles the y/n prompt for a pending operation.
func (m Model) onConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		cmd := m.startPending()
		return m, cmd
	case "n", "esc", "q":
		m.undoing = false
		// Cancelling a batch rename goes back to the tool with the form intact:
		// the whole point of the preview is trying patterns out.
		if m.pending.Op == fileops.OpRename && m.ren.open {
			m.mode = modeRename
			return m, nil
		}
		m.mode = modeNormal
	}
	return m, nil
}

// onEditKey drives the nano-style editor.
func (m Model) onEditKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+s":
		m.saveEditor()
		return m, nil
	case "ctrl+q":
		m.closeEditor()
		return m, nil
	case "esc":
		if m.editor.Value() != m.editOrig {
			m.editStatus = "unsaved — Ctrl+S to save, Ctrl+Q to discard"
			return m, nil
		}
		m.closeEditor()
		return m, nil
	}
	m.editStatus = ""
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

// enterOrOpen descends into a directory, or opens an archive for browsing.
func (m *Model) enterOrOpen() {
	p := &m.panes[m.active]
	if !p.InArchive() {
		if cur, ok := p.Current(); ok && !cur.IsDir && fileops.Browsable(cur.Name) {
			if err := p.EnterArchive(filepath.Join(p.Path, cur.Name)); err != nil {
				m.errText = "cannot open archive: " + err.Error()
			}
			return
		}
	}
	p.Enter()
}

// loadCurrent reads the highlighted file (real or in-archive) into memory.
func (m *Model) loadCurrent() (data []byte, title string, err error) {
	p := &m.panes[m.active]
	if p.IsRemote() {
		return nil, "", fmt.Errorf("view and edit are local-only — copy the file across first")
	}
	if p.InArchive() {
		mem, ok := p.CurrentMemberPath()
		if !ok {
			return nil, "", fmt.Errorf("select a file to open")
		}
		data, err = fileops.ReadMember(p.ArchivePath(), mem)
		return data, mem, err
	}
	cur, ok := p.Current()
	if !ok || cur.IsDir {
		return nil, "", fmt.Errorf("select a file to open")
	}
	full := filepath.Join(p.Path, cur.Name)
	data, err = os.ReadFile(full)
	return data, cur.Name, err
}

// openEditor loads the current file into the editor and remembers where to save.
func (m *Model) openEditor() tea.Cmd {
	data, title, err := m.loadCurrent()
	if err != nil {
		m.errText = err.Error()
		return nil
	}
	if isBinary(data) {
		m.errText = "not a text file: " + title
		return nil
	}

	p := &m.panes[m.active]
	m.edit = editTarget{title: title}
	if p.InArchive() {
		mem, _ := p.CurrentMemberPath()
		m.edit.archive = p.ArchivePath()
		m.edit.member = mem
	} else {
		cur, _ := p.Current()
		m.edit.realPath = filepath.Join(p.Path, cur.Name)
	}

	m.editOrig = string(data)
	m.editStatus = ""
	m.editor.SetValue(string(data))
	m.mode = modeEdit
	return m.editor.Focus()
}

// saveEditor writes the buffer back to its origin (file or archive member).
func (m *Model) saveEditor() {
	data := []byte(m.editor.Value())

	var err error
	if m.edit.archive != "" {
		err = fileops.WriteMember(m.edit.archive, m.edit.member, data)
	} else {
		perm := os.FileMode(0o644)
		if fi, e := os.Stat(m.edit.realPath); e == nil {
			perm = fi.Mode().Perm()
		}
		err = os.WriteFile(m.edit.realPath, data, perm)
	}
	if err != nil {
		m.errText = "save failed: " + err.Error()
		m.editStatus = "save failed"
		return
	}

	m.editOrig = string(data) // buffer is now clean
	m.editStatus = "saved"
	m.panes[m.active].Refresh()
}

func (m *Model) closeEditor() {
	m.mode = modeNormal
	m.editor.Blur()
	m.editStatus = ""
}

// beginOp assembles a job from the active pane's selection and asks for
// confirmation. Source = active pane; destination = the other pane (except
// delete, which has none, and unwrap, which extracts in place).
func (m *Model) beginOp(op fileops.Op) {
	src := &m.panes[m.active]
	if src.IsRemote() || m.panes[1-m.active].IsRemote() {
		if op == fileops.OpTrash {
			op = fileops.OpDelete // there is no trash on the far side
		}
		m.beginRemoteOp(op)
		return
	}
	if src.InArchive() {
		m.errText = "not available inside an archive — unpack it first"
		return
	}
	// Pack/unpack need a real destination directory; copy/move can target an
	// archive (handled below as add-to-archive).
	if (op == fileops.OpPack || op == fileops.OpUnpack) && m.panes[1-m.active].InArchive() {
		m.errText = "destination pane is inside an archive"
		return
	}
	names := src.SelectedNames()
	if len(names) == 0 {
		return
	}

	srcs := make([]string, 0, len(names))
	for _, n := range names {
		srcs = append(srcs, filepath.Join(src.Path, n))
	}
	dst := &m.panes[1-m.active]
	other := dst.Path

	m.willOverwrite = false
	job := fileops.Job{Op: op, Srcs: srcs}

	switch op {
	case fileops.OpCopy, fileops.OpMove:
		if dst.InArchive() {
			// Copy/move real files into the archive at its current virtual dir.
			job = fileops.Job{
				Op:   fileops.OpAddToArchive,
				Srcs: srcs,
				Dest: dst.ArchivePath(),
				VDir: dst.VPath(),
				Move: op == fileops.OpMove,
			}
			break
		}
		if other == src.Path {
			m.errText = "source and destination are the same directory"
			return
		}
		job.Dest = other
		m.willOverwrite = anyExist(other, names)

	case fileops.OpDelete:
		// destructive; no destination
	case fileops.OpTrash:
		if err := trashAvailable(); err != nil {
			m.errText = err.Error()
			return
		}

	case fileops.OpUnpack, fileops.OpUnwrap:
		if bad := firstNonArchive(names); bad != "" {
			m.errText = "not a supported archive: " + bad
			return
		}
		if op == fileops.OpUnpack {
			if other == src.Path {
				m.errText = "source and destination are the same directory"
				return
			}
			job.Dest = other
		} else {
			job.Dest = src.Path // unwrap = extract in place
		}

	case fileops.OpPack:
		if other == src.Path {
			m.errText = "source and destination are the same directory"
			return
		}
		// Packing has settings worth choosing, so it opens the pack dialog and
		// reaches the confirm prompt from there.
		m.openPack(srcs, names, other)
		return
	}

	m.pending = job
	m.mode = modeConfirm
}

// anyExist reports whether any of names already exists under dir.
func anyExist(dir string, names []string) bool {
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, n)); err == nil {
			return true
		}
	}
	return false
}

// deleteOp is what F8 means here: the trash when it is switched on and usable,
// an unlink when it is not. A remote pane has no trash to speak of, and the
// remote branch of beginOp turns either one into OpRemoteDelete.
func (m Model) deleteOp() fileops.Op {
	if m.deleteToTrash && trash.Available() {
		return fileops.OpTrash
	}
	return fileops.OpDelete
}

// trashAvailable explains why a trash delete cannot run, if it cannot.
func trashAvailable() error {
	if trash.Available() {
		return nil
	}
	return fmt.Errorf("no usable trash directory — D deletes permanently")
}

// localExists reports whether anything is at path on this machine.
func localExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// firstNonArchive returns the first name that isn't a supported archive, or "".
func firstNonArchive(names []string) string {
	for _, n := range names {
		if !fileops.IsArchive(n) {
			return n
		}
	}
	return ""
}

// startPending launches the confirmed job and enters progress mode. The job
// gets a context of its own, which is what Esc cancels; see onProgressKey.
func (m *Model) startPending() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancelOp = cancel
	m.cancelling = false

	ch := fileops.Run(ctx, m.pending)
	m.progressCh = ch
	m.progress = fileops.Progress{}
	m.mode = modeProgress
	return waitCmd(ch)
}

// onProgressKey is what a keypress means while an operation runs. Stopping it is
// the only thing on offer — every other key stays locked out, so a stray press
// cannot start a second job on top of the one in flight.
//
// Ctrl+C stops the job rather than quitting. Leaving mid-write would abandon a
// half-copied file with nothing in the undo history to say so; the job unwinds
// first, and a second Ctrl+C from the normal screen quits as it always did.
// Nothing changes mode here: the job reports what it managed to do through the
// same Result as any other ending, and finishOp takes it from there.
func (m Model) onProgressKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		if m.cancelOp != nil && !m.cancelling {
			m.cancelOp()
			m.cancelling = true
		}
	}
	return m, nil
}

// finishOp refreshes both panes and surfaces any error. A remote pane refreshes
// over the network, so this returns commands rather than doing it inline.
func (m Model) finishOp(res fileops.Result) (tea.Model, tea.Cmd) {
	m.mode = modeNormal
	m.progressCh = nil

	// The job is over either way, so its context has nothing left to govern.
	// Releasing it here rather than only on the cancel path is what keeps the
	// context from outliving the work it was made for.
	if m.cancelOp != nil {
		m.cancelOp()
		m.cancelOp = nil
	}
	m.cancelling = false

	// An encrypted archive is not a failure yet: ask for the password and run
	// the same job again with it.
	if errors.Is(res.Err, fileops.ErrNeedPassword) {
		cmd := m.askUnpackPassword(m.pending)
		return m, cmd
	}

	wasUndo := m.undoing
	m.undoing = false

	switch {
	case res.Op == fileops.OpRename:
		// The batch is done; the tool it came from has nothing left to preview,
		// and what it did becomes the top of the undo history.
		m.ren.fields[m.ren.focus].Blur()
		m.ren = renameState{}
		m.pushUndoRename(m.pending.Dest, res.Renamed)
		if n := len(res.Renamed); n > 0 {
			m.noticeText = fmt.Sprintf("renamed %s · ctrl+z to undo", rename.UndoLabel(res.Renamed))
		}
	case res.Op == fileops.OpRenameUndo:
		m.popUndo()
		if n := len(res.Renamed); n > 0 {
			m.noticeText = "put " + rename.UndoLabel(res.Renamed) + " back"
		}
	case wasUndo:
		// The reversal ran: the entry it came from is spent either way.
		m.popUndo()
		if res.Err == nil {
			m.noticeText = "put it back"
		}
	default:
		m.recordUndo(res)
		if notice := undoNotice(res); notice != "" {
			m.noticeText = notice
		}
	}

	var cmds []tea.Cmd
	for i := range m.panes {
		m.panes[i].ClearSelection()
		if cmd := m.refreshPane(i); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	switch {
	case errors.Is(res.Err, fileops.ErrCancelled):
		// Cancelling at a collision is a decision, not a failure. Whatever was
		// copied before it still counts, and is still in the undo history.
		m.noticeText = res.Op.String() + " cancelled"
	case res.Err != nil:
		m.errText = res.Op.String() + " failed: " + res.Err.Error()
	}
	return m, tea.Batch(cmds...)
}

// undoNotice is what to say about a job that can now be put back.
func undoNotice(res fileops.Result) string {
	switch res.Op {
	case fileops.OpCopy:
		if n := len(res.Created); n > 0 {
			return fmt.Sprintf("copied %s · ctrl+z removes the copies", countLabel(n))
		}
	case fileops.OpMove:
		if n := len(res.Moved); n > 0 {
			return fmt.Sprintf("moved %s · ctrl+z puts it back", countLabel(n))
		}
	case fileops.OpTrash:
		if n := len(res.Trashed); n > 0 {
			return fmt.Sprintf("%s to trash · ctrl+z puts it back", countLabel(n))
		}
	}
	return ""
}

// resizePanes splits the terminal into two panes above the status bar and sizes
// the view/edit components to the full body area.
func (m *Model) resizePanes() {
	bodyH := m.height - 1 // reserve one line for the status bar
	leftW := m.width / 2
	m.panes[0].SetSize(leftW, bodyH)
	m.panes[1].SetSize(m.width-leftW, bodyH)

	compH := m.height - 2 // header + footer
	if compH < 1 {
		compH = 1
	}
	m.viewport.Width = m.width
	m.viewport.Height = compH
	if len(m.viewer.raw) > 0 {
		// Wrapping and the hex dump are laid out against the width, so a resize
		// is a re-render, not just a smaller window onto the old one.
		m.refreshViewer()
	}
	m.editor.SetWidth(m.width)
	m.editor.SetHeight(compH)
}

// waitCmd blocks on the next value from the operation channel and delivers it
// as a message; it returns nil once the channel is closed.
func waitCmd(ch <-chan any) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// isBinary reports whether data looks non-textual (contains a NUL byte).
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8000 {
		n = 8000
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}
