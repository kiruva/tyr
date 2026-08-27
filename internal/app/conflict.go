package app

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/pane"
	"github.com/kiruva/tyr/internal/ui"
)

// When a copy or a move lands on a name that is taken, the job stops and asks.
// The dialog puts the two files side by side — size and time, with the newer
// one marked — because that is the whole basis for the decision, and every
// answer can be given once or for the rest of the batch.

// conflictState is the question currently on screen.
type conflictState struct {
	c    fileops.Conflict
	open bool
}

// onConflict shows the question and stops reading the job's channel until it is
// answered: the job is blocked on the reply, so there is nothing else to read.
func (m Model) onConflict(c fileops.Conflict) (tea.Model, tea.Cmd) {
	m.conflict = conflictState{c: c, open: true}
	m.mode = modeConflict
	return m, nil
}

// onConflictKey answers the question and lets the job carry on.
func (m Model) onConflictKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	action, all, ok := conflictAnswer(msg.String())
	if !ok {
		return m, nil
	}
	return m.answerConflict(action, all)
}

// conflictAnswer maps a keypress onto an answer. The capital of each key is the
// same answer for every remaining collision.
func conflictAnswer(key string) (action fileops.ConflictAction, all, ok bool) {
	switch key {
	case "o":
		return fileops.ConflictOverwrite, false, true
	case "O":
		return fileops.ConflictOverwrite, true, true
	case "s":
		return fileops.ConflictSkip, false, true
	case "S":
		return fileops.ConflictSkip, true, true
	case "b":
		return fileops.ConflictKeepBoth, false, true
	case "B":
		return fileops.ConflictKeepBoth, true, true
	case "u":
		return fileops.ConflictNewer, false, true
	case "U":
		return fileops.ConflictNewer, true, true
	case "esc", "c", "ctrl+c":
		return fileops.ConflictCancel, false, true
	}
	return 0, false, false
}

// answerConflict sends the decision back to the job and resumes the progress
// view, which is where the next message — another conflict, or the result —
// arrives.
func (m Model) answerConflict(action fileops.ConflictAction, all bool) (tea.Model, tea.Cmd) {
	reply := m.conflict.c.Reply
	m.conflict = conflictState{}
	m.mode = modeProgress

	if reply != nil {
		reply <- fileops.Resolution{Action: action, All: all}
	}
	return m, waitCmd(m.progressCh)
}

// renderConflict draws the question: what is coming in, what is already there,
// and every way out of it.
func (m Model) renderConflict() string {
	c := m.conflict.c
	name := filepath.Base(c.Dst)

	title := ui.Danger.Render("Already there")
	subject := ui.Faint.Render("in "+ui.TruncTail(filepath.Dir(c.Dst), contentWidth-3)) + "\n" +
		ui.TruncTail(name, contentWidth)

	lines := []string{
		title,
		"",
		subject,
		"",
		conflictSide("incoming", c.SrcInfo, c.SrcNewer()),
		conflictSide("already there", c.DstInfo, !c.SrcNewer()),
		"",
		conflictChoice("o", "overwrite") + "   " + conflictChoice("s", "skip"),
		conflictChoice("b", "keep both") + "   " + conflictChoice("u", "overwrite if newer"),
		"",
		ui.Faint.Render("capitals answer the rest · esc cancels"),
	}
	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// conflictSide is one file's line in the comparison.
func conflictSide(label string, meta fileops.Meta, newer bool) string {
	const labelW = 15

	detail := "missing"
	if !meta.Missing {
		detail = fmt.Sprintf("%-9s %s", pane.HumanSize(meta.Size), meta.ModTime.Format("2006-01-02 15:04"))
		if meta.IsDir {
			detail = fmt.Sprintf("%-9s %s", "folder", meta.ModTime.Format("2006-01-02 15:04"))
		}
	}
	if newer && !meta.Missing {
		detail += "  " + ui.HelpKey.Render("newer")
	}
	return ui.Faint.Render(ui.PadRight(label, labelW)) + detail
}

func conflictChoice(key, label string) string {
	return ui.DialogHint.Render(key) + " " + ui.PadRight(label, 20)
}
