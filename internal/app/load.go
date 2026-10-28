package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/pane"
	"github.com/kiruva/tyr/internal/syntax"
)

// maxViewBytes is how much of a file the viewer and editor hold. The viewer
// shows the start of anything bigger and says so; the editor refuses it, since
// saving what it holds would cut the file short.
const maxViewBytes = 16 << 20

// Opening a file reads it off the UI thread. The checks that can say no at
// once — remote, a folder, a pipe — are made first, where the answer is shown
// straight away; the read, and for an image the metadata decoding, happen in a
// command whose reply opens the viewer or the editor. A pipe or a device is
// refused outright: reading one waits for a writer, or never ends.

// loadPurpose is what the bytes are for.
type loadPurpose int

const (
	loadView loadPurpose = iota
	loadEdit
)

// loadedMsg carries a read file back to the UI.
type loadedMsg struct {
	seq     int
	purpose loadPurpose
	title   string
	target  editTarget  // where an edit is saved to
	data    []byte      // for the editor
	view    viewerState // for the viewer, already laid out
	err     error
}

// errTooBigToEdit is what the editor says about a file it would truncate.
var errTooBigToEdit = fmt.Errorf("too big to edit here (over %s)", pane.HumanSize(maxViewBytes))

// startLoad checks the highlighted entry and returns the command that reads
// it, or nil with errText set when it cannot be opened.
func (m *Model) startLoad(purpose loadPurpose) tea.Cmd {
	p := &m.panes[m.active]
	if p.IsRemote() {
		m.errText = "view and edit are local-only — copy the file across first"
		return nil
	}

	var target editTarget
	if p.InArchive() {
		mem, ok := p.CurrentMemberPath()
		if !ok {
			m.errText = "select a file to open"
			return nil
		}
		target = editTarget{title: mem, archive: p.ArchivePath(), member: mem}
	} else {
		cur, ok := p.Current()
		if !ok || cur.IsDir {
			m.errText = "select a file to open"
			return nil
		}
		full := filepath.Join(p.Path, cur.Name)
		info, err := os.Stat(full)
		if err != nil {
			m.errText = err.Error()
			return nil
		}
		if !info.Mode().IsRegular() {
			m.errText = cur.Name + " is not a regular file — a pipe, socket or device has nothing to show"
			return nil
		}
		target = editTarget{title: cur.Name, realPath: full}
	}
	return m.loadCmd(target, purpose)
}

// loadCmd reads target off the UI thread for purpose.
func (m *Model) loadCmd(target editTarget, purpose loadPurpose) tea.Cmd {
	m.loadSeq++
	seq := m.loadSeq
	return func() tea.Msg {
		msg := loadedMsg{seq: seq, purpose: purpose, title: target.title, target: target}
		if target.realPath != "" {
			// Stamped before the read: a write landing during it is then a
			// change the save notices, not one it misses.
			msg.target.stamp = stampOf(target.realPath)
		}
		data, truncated, err := readTarget(target)
		switch {
		case err != nil:
			msg.err = err
		case purpose == loadEdit && truncated:
			msg.err = fmt.Errorf("%s: %w", target.title, errTooBigToEdit)
		case purpose == loadEdit:
			msg.data = data
		default:
			msg.view = newViewer(target.title, data, syntax.Detect(target.title))
			msg.view.source = target
			if truncated {
				msg.view.status = "showing the first " + pane.HumanSize(maxViewBytes) + " only"
			}
		}
		return msg
	}
}

// onLoaded opens what was read — unless another open has started since, or the
// app has moved on to something else.
func (m Model) onLoaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	if msg.seq != m.loadSeq || (m.mode != modeNormal && m.mode != modeView) {
		return m, nil
	}
	if msg.err != nil {
		m.errText = msg.err.Error()
		return m, nil
	}
	if msg.purpose == loadView {
		m.installViewer(msg.view)
		return m, nil
	}
	if isBinary(msg.data) {
		m.errText = "not a text file: " + msg.title
		return m, nil
	}
	// The editor holds text as runes; bytes that are not UTF-8 would not
	// survive the trip and are not opened for editing.
	if !utf8.Valid(msg.data) {
		m.errText = msg.title + " is not UTF-8 text — it cannot be edited here without changing it"
		return m, nil
	}
	m.edit = msg.target
	m.editStatus = ""
	text := string(msg.data)
	first, _, _ := strings.Cut(text, "\n")
	m.editor.SetText(text, syntax.DetectText(msg.title, first))
	m.mode = modeEdit
	return m, nil
}

// readTarget reads up to maxViewBytes of what target names, reporting whether
// there was more.
func readTarget(target editTarget) ([]byte, bool, error) {
	if target.archive != "" {
		data, err := fileops.ReadMember(target.archive, target.member)
		if len(data) > maxViewBytes {
			return data[:maxViewBytes], true, err
		}
		return data, false, err
	}
	// Checked again here: this may be a second open of what the viewer
	// showed, and the name may since have become something else.
	if info, err := os.Stat(target.realPath); err != nil {
		return nil, false, err
	} else if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s is not a regular file", target.title)
	}
	return readCapped(target.realPath, maxViewBytes)
}

// readCapped reads up to limit bytes of path, reporting whether there was more.
func readCapped(path string, limit int) ([]byte, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	if len(data) > limit {
		return data[:limit], true, nil
	}
	return data, false, nil
}
