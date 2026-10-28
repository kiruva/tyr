package app

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fsutil"
	"github.com/kiruva/tyr/internal/syntax"
	"github.com/kiruva/tyr/internal/termsafe"
)

// Ctrl+O in the editor writes the buffer to another file, named in a field in
// the footer, and goes on editing that one — the file it came from is left as
// it was last saved. The field starts with the file's own path; a bare name
// lands beside it (beside the archive, for a member being edited). A file
// that is already there is only overwritten on a second Enter.

// saveAs is the save-as prompt.
type saveAs struct {
	on      bool
	field   textinput.Model
	confirm string // the path a second Enter overwrites
}

func (m *Model) openSaveAs() tea.Cmd {
	start := m.edit.realPath
	if m.edit.archive != "" {
		start = filepath.Join(filepath.Dir(m.edit.archive), path.Base(m.edit.member))
	}
	field := newInput("file to save to", false, max(m.width/2, 20))
	field.SetValue(start)
	field.CursorEnd()
	cmd := field.Focus()
	m.editAs = saveAs{on: true, field: field}
	m.editStatus, m.editJSON = "", false
	return cmd
}

func (m Model) onSaveAsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editAs.on = false
		m.editStatus = ""
		return m, nil
	case "enter":
		m.saveAsTo(m.saveAsPath())
		return m, nil
	}
	var cmd tea.Cmd
	m.editAs.field, cmd = m.editAs.field.Update(msg)
	m.editAs.confirm, m.editStatus = "", ""
	return m, cmd
}

// saveAsPath is the field's path made whole: ~ is the home directory, and a
// relative path is taken from the folder the file being edited is in.
func (m Model) saveAsPath() string {
	p := strings.TrimSpace(m.editAs.field.Value())
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~"+string(os.PathSeparator)) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if !filepath.IsAbs(p) {
		base := filepath.Dir(m.edit.realPath)
		if m.edit.archive != "" {
			base = filepath.Dir(m.edit.archive)
		}
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

// saveAsTo writes the buffer to target and makes it the file being edited.
func (m *Model) saveAsTo(target string) {
	if target == "" {
		m.editStatus = "type the file to save to"
		return
	}
	if m.edit.archive == "" && target == m.edit.realPath {
		m.editAs.on = false
		m.saveEditor()
		return
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(m.edit.realPath); m.edit.archive == "" && err == nil {
		perm = fi.Mode().Perm()
	}
	info, err := os.Stat(target)
	switch {
	case err == nil && info.IsDir():
		m.editStatus = filepath.Base(target) + " is a folder"
		return
	case err == nil && m.editAs.confirm != target:
		m.editAs.confirm = target
		m.editStatus = filepath.Base(target) + " exists — Enter again to overwrite it"
		return
	case err == nil:
		perm = info.Mode().Perm() // overwriting keeps the file's own permissions
	case !errors.Is(err, fs.ErrNotExist):
		m.editStatus = "cannot save there: " + err.Error()
		return
	}
	if err := fsutil.WriteFile(target, []byte(m.editor.Text()), perm); err != nil {
		m.editStatus = "save failed: " + err.Error()
		return
	}

	m.edit = editTarget{realPath: target, title: filepath.Base(target), stamp: stampOf(target)}
	m.editAs.on = false
	m.editor.MarkSaved()
	m.editor.SetLang(syntax.DetectText(target, m.editor.FirstLine()))
	m.editStatus = "saved as " + target
	for i := range m.panes {
		if !m.panes[i].IsRemote() {
			m.panes[i].Refresh()
		}
	}
}

// saveAsHint is the footer while the prompt is open.
func (m Model) saveAsHint() string {
	hint := "enter save · esc cancel"
	if m.editStatus != "" {
		hint = termsafe.Line(m.editStatus)
	}
	return " save as: " + m.editAs.field.View() + "  " + hint
}
