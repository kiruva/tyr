package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/rename"
	"github.com/kiruva/tyr/internal/syntax"
	"github.com/kiruva/tyr/internal/termsafe"
	"github.com/kiruva/tyr/internal/trash"
	"github.com/kiruva/tyr/internal/ui"
)

// View implements tea.Model. Every frame goes through termsafe.Frame on its way
// out: names, paths, file contents and error text all come from outside, and a
// render path that forgot to escape one must still not be able to hand the
// terminal a command.
func (m Model) View() string {
	return termsafe.Frame(m.render())
}

// render draws the screen for the mode the app is in.
func (m Model) render() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}

	body := lipgloss.JoinHorizontal(
		lipgloss.Top,
		m.panes[0].View(m.active == 0),
		m.panes[1].View(m.active == 1),
	)
	base := lipgloss.JoinVertical(lipgloss.Left, body, m.statusBar())

	md := m.modal()
	switch {
	case md.view == nil:
		return base
	case md.fullScreen:
		return md.view(m)
	default:
		return overlay(m.width, m.height, md.view(m))
	}
}

// overlayRows is how many lines of a full-height overlay fit on the terminal,
// after the border, the padding, the header and the footer have had theirs.
func (m Model) overlayRows() int {
	return max(m.height-8, 3)
}

// renderViewer draws the read-only pager full-screen: what is open and how it
// is being shown along the top, what can be done to it along the bottom.
func (m Model) renderViewer() string {
	right := m.viewerFlags()
	left := " view · " + ui.TruncTail(m.viewTitle, max(m.width-lipgloss.Width(right)-10, 8))
	header := ui.StatusBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)

	return lipgloss.JoinVertical(lipgloss.Left, header, m.viewport.View(), m.viewerFooter())
}

// viewerFlags says which of the pager's switches are on, and what the file is
// being highlighted as.
func (m Model) viewerFlags() string {
	var on []string
	if m.viewer.hex {
		on = append(on, "hex")
	} else if m.viewer.meta {
		on = append(on, "metadata")
	}
	if m.viewer.wrap {
		on = append(on, "wrap")
	}
	if m.viewer.numbers && !m.viewer.hex {
		on = append(on, "numbers")
	}
	if m.viewer.colour && !m.viewer.hex && m.viewer.lang != syntax.None {
		on = append(on, m.viewer.lang.String())
	}
	if len(on) == 0 {
		return " "
	}
	return strings.Join(on, " · ") + " "
}

// viewerFooter is the search field, whatever the pager has to say, or the keys.
func (m Model) viewerFooter() string {
	if m.viewer.prompting {
		left := " find: " + m.viewer.input.View()
		right := "enter keep · esc clear"
		return ui.StatusBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)
	}
	if m.viewer.status != "" {
		left := " " + ui.TruncTail(m.viewer.status, max(m.width-12, 8))
		right := fmt.Sprintf("%3.0f%% ", m.viewport.ScrollPercent()*100)
		return ui.NoticeBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)
	}

	left := " / find · n next · w wrap · # numbers · x hex · s colour · e edit · q close"
	right := fmt.Sprintf("%3.0f%% ", m.viewport.ScrollPercent()*100)
	return ui.StatusBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)
}

// renderEditor draws the editor full-screen.
func (m Model) renderEditor() string {
	name := m.edit.title
	if m.editor.Dirty() {
		name += " *"
	}
	header := ui.StatusBar.Width(m.width).Render(" edit · " + ui.TruncTail(name, m.width-8))

	hint := " ^S save · ^Q quit · ^F find · ^Z undo · ^C/^X/^V · ^T json"
	switch {
	case m.editJSON:
		hint = editJSONHint()
	case m.editStatus != "":
		hint = " " + termsafe.Line(m.editStatus)
	}
	line, col := m.editor.Cursor()
	where := fmt.Sprintf("Ln %d, Col %d ", line, col)
	footer := ui.StatusBar.Width(m.width).Render(hint + spacer(m.width, hint, where) + where)
	return lipgloss.JoinVertical(lipgloss.Left, header, m.editor.View(), footer)
}

// overlay centers a modal box on a blank screen of the given size.
func overlay(w, h int, dialog string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, dialog)
}

func (m Model) statusBar() string {
	if m.mode == modeFilter {
		return m.renderFilterBar()
	}
	if m.errText != "" {
		return ui.ErrorBar.Width(m.width).Render(" " + barText(m.errText, m.width-1))
	}
	if m.noticeText != "" {
		return ui.NoticeBar.Width(m.width).Render(" " + ui.TruncTail(m.noticeText, m.width-1))
	}

	p := &m.panes[m.active]
	left := p.Title()

	if m.mode == modeAddress {
		right := "enter go · tab complete · esc cancel"
		return ui.StatusBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)
	}

	if p.Loading() {
		right := "connecting…"
		return ui.StatusBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)
	}

	right := fmt.Sprintf("%d items", len(p.Entries))
	if n := p.SelectedCount(); n > 0 {
		right += fmt.Sprintf(" · %d selected", n)
	}
	if f := p.Filter(); f != "" {
		right += " · filter:" + ui.TruncTail(f, 16)
	}
	right += fmt.Sprintf(" · sort:%s", p.SortModeLabel())
	if p.HiddenShown() {
		right += " · hidden"
	}
	right += " · ? help"

	return ui.StatusBar.Width(m.width).Render(left + spacer(m.width, left, right) + right)
}

// barText fits an error to the one line the status bar has. Errors carry text
// from outside — a remote's stderr, a tool's complaint, the name that failed —
// so it is escaped (TruncHead does that), and its lines are joined into one: a
// multi-line message would push the panes up off the screen. The front is kept
// when it is cut, since that is where an error says what went wrong.
func barText(s string, width int) string {
	lines := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' })
	return ui.TruncHead(strings.Join(lines, " · "), width)
}

// spacer is the filler that pushes right up against the edge of a bar of the
// given total width, never closing the gap entirely.
func spacer(total int, left, right string) string {
	pad := total - lipgloss.Width(left) - lipgloss.Width(right)
	if pad < 1 {
		pad = 1
	}
	return strings.Repeat(" ", pad)
}

func (m Model) renderConfirm() string {
	j := m.pending
	n := len(j.Srcs)

	var title, body string
	switch j.Op {
	case fileops.OpDelete:
		if m.undoing {
			title = ui.Danger.Render(fmt.Sprintf("Remove %d copied %s?", n, items(n)))
			body = "The trash cannot take them, so they are deleted for good."
			break
		}
		title = ui.Danger.Render(fmt.Sprintf("Delete %d %s?", n, items(n)))
		body = "This cannot be undone."
	case fileops.OpTrash:
		if m.undoing {
			title = ui.DialogTitle.Render(fmt.Sprintf("Remove %d copied %s?", n, items(n)))
			body = "They go to the trash → " + ui.TruncTail(trash.Describe(), 30)
			break
		}
		title = ui.DialogTitle.Render(fmt.Sprintf("Move %d %s to the trash?", n, items(n)))
		body = "→ " + ui.TruncTail(trash.Describe(), 44) + "\n" + ui.Faint.Render("ctrl+z puts it back · D deletes for good")
	case fileops.OpRestore:
		title = ui.DialogTitle.Render("Restore from the trash")
		body = "put " + termsafe.Line(trash.Label(j.Trash)) + " back"
	case fileops.OpSync:
		toRight, toLeft := syncDirections(j.Pairs, j.Dest)
		title = ui.DialogTitle.Render("Synchronize " + syncLabel(j))
		body = fmt.Sprintf("%d → right\n%d ← left", toRight, toLeft) + "\n" +
			ui.Faint.Render("files in the way are replaced")
	case fileops.OpMovePairs:
		title = ui.DialogTitle.Render(fmt.Sprintf("Move %d %s back", len(j.Pairs), items(len(j.Pairs))))
		body = "→ " + ui.TruncTail(j.Dest, 44)
	case fileops.OpPack:
		title = ui.DialogTitle.Render(fmt.Sprintf("Pack %d %s", n, items(n)))
		body = "→ " + ui.TruncTail(j.Out, 44) + "\n" + ui.Faint.Render(packSummary(j.Pack))
		if j.Pack.Weak() {
			body += "\n" + ui.Danger.Render("ZipCrypto is weak — install p7zip for AES-256")
		}
	case fileops.OpConvert:
		title = ui.DialogTitle.Render(fmt.Sprintf("Convert %d %s", n, items(n)))
		body = "→ " + ui.TruncTail(j.Dest, 44)
		if j.Convert.Encoder != "" {
			body += "\n" + ui.Faint.Render(j.Convert.Summary())
		}
		if j.Convert.Images {
			body += "\n" + ui.Faint.Render(j.Convert.ImageSummary())
		}
		if m.convertInPlace {
			body += "\n" + ui.Danger.Render("Images are stripped in place: the originals are") +
				"\n" + ui.Danger.Render("overwritten, and ctrl+z cannot bring the metadata back.")
		}
	case fileops.OpRotate:
		// Only an undo asks first; a turn from the dialog just happens.
		title = ui.DialogTitle.Render(fmt.Sprintf("Turn %d %s back", n, images(n)))
		body = termsafe.Line(j.Turn.String())
	case fileops.OpChmod:
		title = ui.Danger.Render("Change permissions of everything in " + termsafe.Line(ui.TruncHead(filepath.Base(j.Srcs[0]), 30)) + "?")
		body = fmt.Sprintf("folders → %04o\nfiles → %04o, or %04o if they run",
			fileops.TreeDirMode(j.Mode), fileops.TreeFileMode(j.Mode, 0), fileops.TreeFileMode(j.Mode, 0o100)) +
			"\n" + ui.Faint.Render("This cannot be undone.")
	case fileops.OpUnpack:
		title = ui.DialogTitle.Render(fmt.Sprintf("Unpack %d %s", n, archives(n)))
		body = "→ " + ui.TruncTail(j.Dest, 44)
	case fileops.OpUnwrap:
		title = ui.DialogTitle.Render(fmt.Sprintf("Unpack %d %s here", n, archives(n)))
		body = "→ " + ui.TruncTail(j.Dest, 44)
	case fileops.OpDownload:
		title = ui.DialogTitle.Render(fmt.Sprintf("Download %d %s", n, items(n)))
		body = "from " + ui.TruncTail(j.Host.String(), 44) + "\n→ " + ui.TruncTail(j.Dest, 44)
		if j.Move {
			body += "\n" + ui.Danger.Render("The originals are removed from the host.")
		}
	case fileops.OpUpload:
		title = ui.DialogTitle.Render(fmt.Sprintf("Upload %d %s", n, items(n)))
		body = "→ " + ui.TruncTail(j.Host.Display(j.Dest), 44) +
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
		body = "→ " + ui.TruncTail(j.Dest, 44)
	case fileops.OpRemoteDelete:
		title = ui.Danger.Render(fmt.Sprintf("Delete %d %s on %s?", n, items(n), j.Host.String()))
		body = "This cannot be undone."
	case fileops.OpRename:
		sum := rename.Summarize(j.Renames)
		title = ui.DialogTitle.Render(fmt.Sprintf("Rename %d %s", sum.Renamed, items(sum.Renamed)))
		body = "in " + ui.TruncTail(j.Dest, 44)
		if sum.Conflicts > 0 {
			body += "\n" + ui.Faint.Render(fmt.Sprintf("%d blocked %s skipped", sum.Conflicts, items(sum.Conflicts)))
		}
		body += "\n" + ui.Faint.Render("ctrl+z puts it back")
	case fileops.OpRenameUndo:
		plan := rename.Applicable(j.Renames)
		title = ui.DialogTitle.Render("Undo rename")
		body = "put " + termsafe.Line(rename.UndoLabel(plan)) + " back\n" + ui.Faint.Render("in "+ui.TruncTail(j.Dest, 40))
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
		body = "→ " + ui.TruncTail(dest+"/", 44)
	default: // copy / move
		title = ui.DialogTitle.Render(fmt.Sprintf("%s %d %s", j.Op, n, items(n)))
		body = "→ " + ui.TruncTail(j.Dest, 44)
	}
	if m.undoing {
		if label := m.undoLabel(); label != "" {
			body += "\n" + ui.Faint.Render("undoing the "+termsafe.Line(label))
		}
		if m.undoLeft > 0 {
			body += "\n" + ui.Faint.Render(countLabel(m.undoLeft)+" changed since — left alone")
		}
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
	// A job stops between files, and an archive tool stops when it is killed, so
	// there is a moment where the answer is "asked, not done yet". Saying so
	// beats a dialog that looks like it ignored the key.
	foot := ui.DialogHint.Render("esc") + ui.Faint.Render(" stop")
	if m.cancelling {
		title = ui.DialogTitle.Render("Stopping…")
		foot = ui.Faint.Render("finishing what is in hand")
	}

	var line string
	if p.Total > 0 {
		// The part of the item in hand counts too, so the overall bar creeps
		// forward with it rather than jumping once per file.
		line = progressBar(p.Done*1000+int(p.Part*1000), p.Total*1000, 30) + fmt.Sprintf("  %d / %d", p.Done, p.Total)
	} else {
		// Total unknown (e.g. 7z/rar): show a static bar and a running count.
		line = ui.BarEmpty.Render(strings.Repeat("░", 30)) + fmt.Sprintf("  %d %s", p.Done, items(p.Done))
	}
	cur := ui.Faint.Render(ui.TruncTail(p.Current, 44))
	rows := []string{title, "", line, cur}

	// A job that knows how far into the current item it is gets a second bar
	// for it, under the name: one long file otherwise looks like a hang.
	if p.PartLabel != "" {
		part := ui.Faint.Render("  " + p.PartLabel)
		if p.Part > 0 {
			part = progressBar(int(p.Part*1000), 1000, 30) + part
		} else {
			part = ui.BarEmpty.Render(strings.Repeat("░", 30)) + part
		}
		rows = append(rows, part)
	}
	rows = append(rows, "", foot)
	content := lipgloss.JoinVertical(lipgloss.Left, rows...)
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

func images(n int) string {
	if n == 1 {
		return "image"
	}
	return "images"
}

func archives(n int) string {
	if n == 1 {
		return "archive"
	}
	return "archives"
}
