package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/remote"
	"github.com/kiruva/tyr/internal/ui"
)

// The connection modal keeps one size across every stage, so moving between them
// does not make the box jump around. dialogWidth is the outer box; contentWidth
// is what is left after the dialog style's horizontal padding, and is what the
// rows inside must fit into.
const (
	dialogWidth  = 58
	dialogPadX   = 3
	contentWidth = dialogWidth - 2*dialogPadX

	connNameCol = 12 // width of the name column in the list
)

// renderConn draws whichever stage of the connection modal is current.
func (m Model) renderConn() string {
	switch m.conn.stage {
	case connStageForm:
		return m.renderConnForm()
	case connStagePassword:
		return m.renderConnPassword()
	case connStageHostKey:
		return m.renderConnHostKey()
	case connStageBusy:
		return m.renderConnBusy()
	default:
		return m.renderConnList()
	}
}

func (m Model) renderConnList() string {
	lines := []string{m.connTitle("Connect over ssh"), ""}

	// marker + name column + a space; whatever is left holds the destination.
	labelW := contentWidth - 2 - connNameCol - 1

	for i, c := range m.conn.conns {
		label := c.Label()
		if remoteConnected(c) {
			label = "● " + label // already connected: no password will be asked
		}
		name := padRight(truncHead(c.Name, connNameCol), connNameCol)
		label = padRight(truncTail(label, labelW), labelW)

		if i != m.conn.cursor {
			lines = append(lines, "  "+name+" "+ui.Faint.Render(label))
			continue
		}
		marker := "▸ "
		if m.conn.deleting {
			marker = ui.Danger.Render("✗ ")
		}
		lines = append(lines, marker+ui.Cursor.Render(name+" "+label))
	}

	newRow := padRight("+ new connection…", contentWidth-2)
	if m.conn.cursor >= len(m.conn.conns) {
		lines = append(lines, "▸ "+ui.Cursor.Render(newRow))
	} else {
		lines = append(lines, "  "+newRow)
	}

	footer := ui.Faint.Render("enter connect · n new · e edit · d delete · esc")
	if m.conn.deleting {
		name := m.conn.conns[m.conn.cursor].Name
		footer = ui.Danger.Render("delete "+name+"?") + ui.Faint.Render("  y / n")
	}

	return connBox(lines, m.conn.status, footer)
}

func (m Model) renderConnForm() string {
	title := "New connection"
	if m.conn.editing != "" {
		title = "Edit " + m.conn.editing
	}
	lines := []string{m.connTitle(title), ""}

	for i := connField(0); i < fieldCount; i++ {
		label := padRight(connFieldLabels[i], 9)
		if i == m.conn.field {
			label = ui.HelpKey.Render(label)
		} else {
			label = ui.Faint.Render(label)
		}
		lines = append(lines, label+m.conn.fields[i].View())
	}

	lines = append(lines, "", ui.Faint.Render("the password is asked on connect, never saved"))
	footer := ui.Faint.Render("tab field · enter save & connect · esc back")
	return connBox(lines, m.conn.status, footer)
}

func (m Model) renderConnPassword() string {
	lines := []string{
		m.connTitle("Password"),
		"",
		ui.Faint.Render("for ") + truncTail(m.conn.connSummary(), contentWidth-4),
		"",
		ui.AddrEdit.Width(contentWidth).Render(m.conn.password.View()),
		"",
		ui.Faint.Render("not saved — asked again next time tyr starts"),
	}
	footer := ui.Faint.Render("enter connect · esc back")
	return connBox(lines, m.conn.status, footer)
}

func (m Model) renderConnHostKey() string {
	hk := m.conn.hostKey
	if hk == nil {
		return m.renderConnList()
	}

	// The fingerprint gets a line of its own: it is the whole point of this
	// stage, and a truncated one cannot be compared against anything.
	lines := []string{
		m.connTitle("Unknown host"),
		"",
		truncTail(m.conn.connSummary(), contentWidth),
		ui.Faint.Render("is not in known_hosts. Its key fingerprint is:"),
		"",
		hk.Fingerprint,
		ui.Faint.Render("(" + hk.KeyType + ")"),
		"",
		ui.Faint.Render("Accept only if that matches the host's own key."),
		ui.Faint.Render("Accepting adds it to ~/.ssh/known_hosts."),
	}
	footer := ui.DialogHint.Render("a") + " accept    " + ui.DialogHint.Render("n") + " cancel"
	return connBox(lines, m.conn.status, footer)
}

func (m Model) renderConnBusy() string {
	lines := []string{
		m.connTitle("Connecting"),
		"",
		truncTail(m.conn.connSummary(), contentWidth),
		"",
		ui.Faint.Render("negotiating with the host…"),
	}
	return connBox(lines, m.conn.status, ui.Faint.Render("esc cancel"))
}

// connBox lays out a modal stage: body, an optional status line, then a footer.
func connBox(lines []string, status, footer string) string {
	body := make([]string, 0, len(lines)+4)
	body = append(body, lines...)
	if status != "" {
		body = append(body, "", ui.Danger.Render(wrapTo(status, contentWidth)))
	}
	body = append(body, "", footer)

	// Width is the outer width including the dialog style's padding, so the rows
	// above are sized to contentWidth and land exactly inside it.
	content := lipgloss.JoinVertical(lipgloss.Left, body...)
	return ui.Dialog.Width(dialogWidth).Render(content)
}

// remoteConnected reports whether a saved connection has a live session, which
// the list marks so it is obvious which ones will not ask for a password.
func remoteConnected(c config.Connection) bool {
	return remote.Connected(connHost(c))
}

// truncHead keeps the start of a string, which is what a name wants — the tail
// is what matters for a path, and truncTail already covers that.
func truncHead(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// wrapTo breaks a message onto lines of at most width columns, so a long error
// from the far side does not stretch the dialog.
func wrapTo(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	var out []string
	var line strings.Builder
	for _, word := range strings.Fields(s) {
		switch {
		case line.Len() == 0:
			line.WriteString(word)
		case lipgloss.Width(line.String())+1+lipgloss.Width(word) <= width:
			line.WriteString(" " + word)
		default:
			out = append(out, line.String())
			line.Reset()
			line.WriteString(word)
		}
	}
	if line.Len() > 0 {
		out = append(out, line.String())
	}
	return strings.Join(out, "\n")
}

// connTitle heads a stage, noting which pane the connection will open in — the
// modal covers the screen, so the pane borders cannot show it.
func (m Model) connTitle(title string) string {
	pane := "left pane"
	if m.conn.paneIdx == 1 {
		pane = "right pane"
	}
	head := ui.DialogTitle.Render(title)
	gap := contentWidth - lipgloss.Width(title) - lipgloss.Width(pane)
	if gap < 1 {
		gap = 1
	}
	return head + strings.Repeat(" ", gap) + ui.Faint.Render(pane)
}
