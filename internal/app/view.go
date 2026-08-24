package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/rename"
	"github.com/kiruva/tyr/internal/ui"
)

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.panes[0].View(m.active == 0),
		m.panes[1].View(m.active == 1),
	)
	base := lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar())

	switch m.mode {
	case modeConfirm:
		return overlay(m.width, m.height, m.renderConfirm())
	case modeProgress:
		return overlay(m.width, m.height, m.renderProgress())
	case modeView:
		return m.renderViewer()
	case modeEdit:
		return m.renderEditor()
	case modeHelp:
		return overlay(m.width, m.height, m.renderHelp())
	case modeCaps:
		return overlay(m.width, m.height, m.renderCaps())
	case modeTheme:
		return overlay(m.width, m.height, m.renderThemePicker())
	case modeConn:
		return overlay(m.width, m.height, m.renderConn())
	case modeCreate:
		return overlay(m.width, m.height, m.renderCreate())
	case modeRenameOne:
		return overlay(m.width, m.height, m.renderRenameOne())
	case modeRename:
		return m.renderRename()
	default:
		return base
	}
}

// renderHelp draws the keybinding overlay, generated from the keymap.
func (m Model) renderHelp() string {
	groups := m.keys.groups()
	blocks := make([]string, 0, len(groups))
	for _, g := range groups {
		lines := []string{ui.HelpKey.Render(g.title)}
		for _, b := range g.binds {
			h := b.Help()
			lines = append(lines, "  "+ui.HelpKey.Render(padRight(h.Key, 9))+ui.Faint.Render(h.Desc))
		}
		blocks = append(blocks, lipgloss.JoinVertical(lipgloss.Left, lines...))
	}

	// split the groups across two columns
	mid := (len(blocks) + 1) / 2
	left := joinBlocks(blocks[:mid])
	right := joinBlocks(blocks[mid:])
	cols := lipgloss.JoinHorizontal(lipgloss.Top, left, "     ", right)

	header := ui.DialogTitle.Render("tyr — keys")
	footer := ui.Faint.Render("C capabilities · any key to close")
	content := lipgloss.JoinVertical(lipgloss.Left, header, "", cols, "", footer)
	return ui.Dialog.Render(content)
}

// renderThemePicker lists the themes with a colour swatch each, above a sample
// of the styles the highlighted theme produces.
func (m Model) renderThemePicker() string {
	const nameW = 12

	lines := []string{ui.DialogTitle.Render("Theme"), ""}
	for i, t := range ui.Themes() {
		name := padRight(t.Name, nameW)
		if i == m.themeCursor {
			lines = append(lines, ui.Cursor.Render("▸ "+name)+" "+ui.Swatch(t))
			continue
		}
		lines = append(lines, "  "+name+" "+ui.Swatch(t))
	}

	const sampleW = 24
	sample := lipgloss.JoinVertical(lipgloss.Left,
		ui.DirName.Render(padRight("  documents/", sampleW)),
		ui.Selected.Render(padRight("● selected.txt", sampleW)),
		ui.Cursor.Render(padRight("  cursor.go", sampleW-6)+" 1.2KB"),
		ui.StatusBar.Render(padRight(" 12 items · sort:name", sampleW)),
	)

	footer := ui.Faint.Render("↑/↓ preview · enter apply · esc cancel")
	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.JoinVertical(lipgloss.Left, lines...), "", sample, "", footer)
	return ui.Dialog.Render(content)
}

// joinBlocks stacks help blocks vertically with a blank line between them.
func joinBlocks(blocks []string) string {
	spaced := make([]string, 0, len(blocks)*2)
	for i, b := range blocks {
		if i > 0 {
			spaced = append(spaced, "")
		}
		spaced = append(spaced, b)
	}
	return lipgloss.JoinVertical(lipgloss.Left, spaced...)
}

func padRight(s string, w int) string {
	if gap := w - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// renderViewer draws the read-only pager full-screen.
func (m Model) renderViewer() string {
	header := ui.StatusBar.Width(m.width).Render(" view · " + truncTail(m.viewTitle, m.width-8))
	footer := ui.StatusBar.Width(m.width).Render(
		fmt.Sprintf(" ↑/↓ scroll · e edit · q close%s%3.0f%%",
			strings.Repeat(" ", pad(m.width, 40)), m.viewport.ScrollPercent()*100))
	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View(), footer)
}

// renderEditor draws the nano-style editor full-screen.
func (m Model) renderEditor() string {
	name := m.edit.title
	if m.editor.Value() != m.editOrig {
		name += " *"
	}
	header := ui.StatusBar.Width(m.width).Render(" edit · " + truncTail(name, m.width-8))

	hint := " Ctrl+S save · Ctrl+Q quit"
	if m.editStatus != "" {
		hint += " · " + m.editStatus
	}
	footer := ui.StatusBar.Width(m.width).Render(hint)
	return lipgloss.JoinVertical(lipgloss.Left, header, m.editor.View(), footer)
}

// pad returns filler width so a right-aligned suffix roughly reaches the edge.
func pad(total, used int) int {
	if p := total - used; p > 1 {
		return p
	}
	return 1
}

// overlay centers a modal box on a blank screen of the given size.
func overlay(w, h int, dialog string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialog)
}

func (m Model) statusBar() string {
	if m.errText != "" {
		return ui.ErrorBar.Width(m.width).Render(" " + m.errText)
	}
	if m.noticeText != "" {
		return ui.NoticeBar.Width(m.width).Render(" " + truncTail(m.noticeText, m.width-1))
	}

	p := &m.panes[m.active]
	left := p.Title()

	if m.mode == modeAddress {
		right := "enter go · tab complete · esc cancel"
		pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
		if pad < 1 {
			pad = 1
		}
		return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", pad) + right)
	}

	if p.Loading() {
		right := "connecting…"
		pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
		if pad < 1 {
			pad = 1
		}
		return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", pad) + right)
	}

	right := fmt.Sprintf("%d items", len(p.Entries))
	if n := p.SelectedCount(); n > 0 {
		right += fmt.Sprintf(" · %d selected", n)
	}
	right += fmt.Sprintf(" · sort:%s", p.SortModeLabel())
	if p.HiddenShown() {
		right += " · hidden"
	}
	right += " · ? help"

	pad := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return ui.StatusBar.Width(m.width).Render(left + strings.Repeat(" ", pad) + right)
}

func (m Model) renderConfirm() string {
	j := m.pending
	n := len(j.Srcs)

	var title, body string
	switch j.Op {
	case fileops.OpDelete:
		title = ui.Danger.Render(fmt.Sprintf("Delete %d %s?", n, items(n)))
		body = "This cannot be undone."
	case fileops.OpPack:
		title = ui.DialogTitle.Render(fmt.Sprintf("Pack %d %s", n, items(n)))
		body = "→ " + truncTail(j.Out, 44)
	case fileops.OpUnpack:
		title = ui.DialogTitle.Render(fmt.Sprintf("Unpack %d %s", n, archives(n)))
		body = "→ " + truncTail(j.Dest, 44)
	case fileops.OpUnwrap:
		title = ui.DialogTitle.Render(fmt.Sprintf("Unpack %d %s here", n, archives(n)))
		body = "→ " + truncTail(j.Dest, 44)
	case fileops.OpDownload:
		title = ui.DialogTitle.Render(fmt.Sprintf("Download %d %s", n, items(n)))
		body = "from " + truncTail(j.Host.String(), 44) + "\n→ " + truncTail(j.Dest, 44)
		if j.Move {
			body += "\n" + ui.Danger.Render("The originals are removed from the host.")
		}
	case fileops.OpUpload:
		title = ui.DialogTitle.Render(fmt.Sprintf("Upload %d %s", n, items(n)))
		body = "→ " + truncTail(j.Host.Display(j.Dest), 44) +
			"\n" + ui.Faint.Render("existing files there are replaced")
		if j.Move {
			body += "\n" + ui.Danger.Render("The local originals are removed.")
		}
	case fileops.OpRemoteCopy:
		verb := "Copy"
		if j.Move {
			verb = "Move"
		}
		title = ui.DialogTitle.Render(fmt.Sprintf("%s %d %s on %s", verb, n, items(n), j.Host.String()))
		body = "→ " + truncTail(j.Dest, 44)
	case fileops.OpRemoteDelete:
		title = ui.Danger.Render(fmt.Sprintf("Delete %d %s on %s?", n, items(n), j.Host.String()))
		body = "This cannot be undone."
	case fileops.OpRename:
		sum := rename.Summarize(j.Renames)
		title = ui.DialogTitle.Render(fmt.Sprintf("Rename %d %s", sum.Renamed, items(sum.Renamed)))
		body = "in " + truncTail(j.Dest, 44)
		if sum.Conflicts > 0 {
			body += "\n" + ui.Faint.Render(fmt.Sprintf("%d blocked %s skipped", sum.Conflicts, items(sum.Conflicts)))
		}
		body += "\n" + ui.Faint.Render("ctrl+z puts it back")
	case fileops.OpRenameUndo:
		plan := rename.Applicable(j.Renames)
		title = ui.DialogTitle.Render("Undo rename")
		body = "put " + rename.UndoLabel(plan) + " back\n" + ui.Faint.Render("in "+truncTail(j.Dest, 40))
		if skipped := rename.Summarize(j.Renames).Conflicts; skipped > 0 {
			body += "\n" + ui.Faint.Render(fmt.Sprintf("%d %s cannot be put back", skipped, items(skipped)))
		}
	case fileops.OpAddToArchive:
		verb := "Add"
		if j.Move {
			verb = "Move"
		}
		title = ui.DialogTitle.Render(fmt.Sprintf("%s %d %s into archive", verb, n, items(n)))
		dest := filepath.Base(j.Dest)
		if j.VDir != "" {
			dest += "/" + j.VDir
		}
		body = "→ " + truncTail(dest+"/", 44)
	default: // copy / move
		title = ui.DialogTitle.Render(fmt.Sprintf("%s %d %s", j.Op, n, items(n)))
		body = "→ " + truncTail(j.Dest, 44)
	}
	if m.willOverwrite {
		body += "\n" + ui.Danger.Render("Existing files will be overwritten.")
	}

	prompt := ui.DialogHint.Render("y") + " confirm    " + ui.DialogHint.Render("n") + " cancel"
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", body, "", prompt)
	return ui.Dialog.Render(content)
}

func (m Model) renderProgress() string {
	p := m.progress
	title := ui.DialogTitle.Render(m.pending.Op.Present() + "…")

	var line string
	if p.Total > 0 {
		line = progressBar(p.Done, p.Total, 30) + fmt.Sprintf("  %d / %d", p.Done, p.Total)
	} else {
		// Total unknown (e.g. 7z/rar): show a static bar and a running count.
		line = ui.BarEmpty.Render(strings.Repeat("░", 30)) + fmt.Sprintf("  %d %s", p.Done, items(p.Done))
	}
	cur := ui.Faint.Render(truncTail(p.Current, 44))

	content := lipgloss.JoinVertical(lipgloss.Left, title, "", line, cur)
	return ui.Dialog.Render(content)
}

func progressBar(done, total, width int) string {
	if total <= 0 {
		total = 1
	}
	filled := done * width / total
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return ui.BarFilled.Render(strings.Repeat("█", filled)) +
		ui.BarEmpty.Render(strings.Repeat("░", width-filled))
}

func items(n int) string {
	if n == 1 {
		return "item"
	}
	return "items"
}

func archives(n int) string {
	if n == 1 {
		return "archive"
	}
	return "archives"
}

// truncTail keeps the tail of a path, prefixing "…" when it's too long.
func truncTail(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	if w <= 1 {
		return "…"
	}
	return "…" + string(r[len(r)-(w-1):])
}
