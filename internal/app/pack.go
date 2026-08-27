package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/ui"
)

// The pack dialog collects everything an archive needs before any tool runs:
// the format (only those whose tools are installed), how hard to compress, an
// optional password, and the output name. Enter goes on to the usual confirm
// prompt, so packing still takes one deliberate y.

type packField int

const (
	packFieldFormat packField = iota
	packFieldLevel
	packFieldPassword
	packFieldName
	packFieldCount
)

// packState is the dialog while it is open.
type packState struct {
	formats []fileops.PackFormatInfo
	sel     int // index into formats
	level   int
	focus   packField

	pw   textinput.Model
	name textinput.Model
	// named records that the output name was typed in, after which changing the
	// format stops rewriting it.
	named  bool
	status string

	srcs []string // absolute paths to pack
	dest string   // directory the archive is written to
	base string   // suggested name without an extension
}

// info is the currently selected format.
func (s packState) info() fileops.PackFormatInfo {
	if s.sel < 0 || s.sel >= len(s.formats) {
		return fileops.PackFormatInfo{}
	}
	return s.formats[s.sel]
}

// canEncrypt reports whether the selected format can take a password here.
func (s packState) canEncrypt() bool { return s.info().Encrypt != "" }

// opts is what the selected settings mean to fileops.
func (s packState) opts() fileops.PackOpts {
	i := s.info()
	o := fileops.PackOpts{Format: i.Format, Level: s.level, Encrypt: i.Encrypt}
	if s.canEncrypt() {
		o.Password = s.pw.Value()
	}
	return o
}

// openPack opens the dialog for the pane's selection.
func (m *Model) openPack(srcs, names []string, dest string) {
	formats := fileops.AvailablePackFormats()
	if len(formats) == 0 {
		m.errText = "no archive tool is installed — press C to see what tyr needs"
		return
	}

	s := packState{
		formats: formats,
		sel:     defaultFormat(formats),
		srcs:    srcs,
		dest:    dest,
		base:    packBase(names),
		pw:      newConnInput("none", true, packValueW-1),
		name:    newConnInput("name", false, contentWidth-packLabelW),
	}
	s.level = s.info().DefLevel
	s.name.SetValue(s.base + s.info().Ext)

	m.pack = s
	m.mode = modePack
}

// defaultFormat prefers tar.gz, the format tyr packed before the dialog existed.
func defaultFormat(formats []fileops.PackFormatInfo) int {
	for i, f := range formats {
		if f.Format == fileops.PackTarGz {
			return i
		}
	}
	return 0
}

// packBase is the suggested archive name for a selection: the single entry's
// own name, or "archive" for several.
func packBase(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	return "archive"
}

// onPackKey drives the dialog.
func (m Model) onPackKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closePack()
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.commitPack()
	case "tab", "down":
		return m.movePackFocus(1)
	case "shift+tab", "up":
		return m.movePackFocus(-1)
	case "left", "right":
		// The choice fields answer to ←/→; inside a text field those keys are
		// the cursor's.
		if m.pack.focus == packFieldFormat || m.pack.focus == packFieldLevel {
			m.pack.status = ""
			step := 1
			if msg.String() == "left" {
				step = -1
			}
			if m.pack.focus == packFieldFormat {
				m.cyclePackFormat(step)
			} else {
				m.cyclePackLevel(step)
			}
			return m, nil
		}
	}

	m.pack.status = ""
	var cmd tea.Cmd
	switch m.pack.focus {
	case packFieldPassword:
		m.pack.pw, cmd = m.pack.pw.Update(msg)
	case packFieldName:
		m.pack.name, cmd = m.pack.name.Update(msg)
		m.pack.named = true
	}
	return m, cmd
}

// movePackFocus walks the fields, skipping the password on a format that cannot
// carry one.
func (m Model) movePackFocus(step int) (tea.Model, tea.Cmd) {
	m.pack.status = ""
	f := m.pack.focus
	for i := 0; i < int(packFieldCount); i++ {
		f = packField((int(f) + step + int(packFieldCount)) % int(packFieldCount))
		if f == packFieldPassword && !m.pack.canEncrypt() {
			continue
		}
		if f == packFieldLevel && !m.pack.info().Levels() {
			continue
		}
		break
	}
	m.pack.focus = f
	return m, m.refocusPack()
}

// refocusPack points the cursor at whichever text field has focus.
func (m *Model) refocusPack() tea.Cmd {
	m.pack.pw.Blur()
	m.pack.name.Blur()
	switch m.pack.focus {
	case packFieldPassword:
		return m.pack.pw.Focus()
	case packFieldName:
		return m.pack.name.Focus()
	}
	return nil
}

// cyclePackFormat moves to the next available format, carrying the level and
// name along: the level is clamped into the new format's range, and the
// extension follows the format unless the name was typed by hand.
func (m *Model) cyclePackFormat(step int) {
	n := len(m.pack.formats)
	if n == 0 {
		return
	}
	m.pack.sel = (m.pack.sel + step + n) % n

	i := m.pack.info()
	m.pack.level = i.DefLevel
	if !m.pack.named {
		m.pack.name.SetValue(m.pack.base + i.Ext)
	}
	// A password already typed for .zip is meaningless for .tar.gz; keep it in
	// the field (switching back should not lose it) but move off it.
	if m.pack.focus == packFieldPassword && !m.pack.canEncrypt() {
		m.pack.focus = packFieldFormat
		m.pack.pw.Blur()
	}
}

func (m *Model) cyclePackLevel(step int) {
	i := m.pack.info()
	if !i.Levels() {
		return
	}
	l := m.pack.level + step
	if l < i.MinLevel {
		l = i.MaxLevel
	}
	if l > i.MaxLevel {
		l = i.MinLevel
	}
	m.pack.level = l
}

// commitPack turns the dialog into a pending job and hands it to the confirm
// prompt. A bad name keeps the dialog open with the reason under the fields.
func (m Model) commitPack() (tea.Model, tea.Cmd) {
	name, err := cleanNewName(m.pack.name.Value())
	if err != nil {
		m.pack.status = err.Error()
		m.pack.focus = packFieldName
		return m, m.refocusPack()
	}
	i := m.pack.info()
	if !strings.HasSuffix(strings.ToLower(name), i.Ext) {
		name += i.Ext
	}
	out := filepath.Join(m.pack.dest, name)

	job := fileops.Job{
		Op:   fileops.OpPack,
		Srcs: m.pack.srcs,
		Dest: m.pack.dest,
		Out:  out,
		Pack: m.pack.opts(),
	}
	m.willOverwrite = false
	if _, err := os.Stat(out); err == nil {
		m.willOverwrite = true
	}

	m.pending = job
	m.closePack()
	m.mode = modeConfirm
	return m, nil
}

func (m *Model) closePack() {
	m.mode = modeNormal
	m.pack.pw.Blur()
	m.pack.name.Blur()
	m.pack = packState{} // the password lives no longer than the dialog
}

// Password prompt for an encrypted archive ------------------------------------

// unpackPwState is the prompt shown when an archive turns out to be encrypted.
// The job is kept as it was so it can be run again with the password filled in.
type unpackPwState struct {
	input textinput.Model
	job   fileops.Job
	again bool // a password was already tried and rejected
}

// askUnpackPassword opens the prompt for the job that just came back encrypted.
func (m *Model) askUnpackPassword(job fileops.Job) tea.Cmd {
	again := job.Password != ""
	job.Password = ""
	m.unpackPw = unpackPwState{
		input: newConnInput("", true, contentWidth-2),
		job:   job,
		again: again,
	}
	m.mode = modeUnpackPw
	return m.unpackPw.input.Focus()
}

func (m Model) onUnpackPwKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeUnpackPw()
		m.noticeText = "unpack cancelled"
		return m, nil
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		pw := m.unpackPw.input.Value()
		if pw == "" {
			return m, nil
		}
		job := m.unpackPw.job
		job.Password = pw
		m.closeUnpackPw()
		m.pending = job
		cmd := m.startPending()
		return m, cmd
	}
	var cmd tea.Cmd
	m.unpackPw.input, cmd = m.unpackPw.input.Update(msg)
	return m, cmd
}

func (m *Model) closeUnpackPw() {
	m.mode = modeNormal
	m.unpackPw.input.Blur()
	m.unpackPw = unpackPwState{} // drop the password with the prompt
}

// Views -----------------------------------------------------------------------

// The dialog is a fixed grid: label, value, then a faint note about the value
// right-aligned to the edge, so nothing moves as the fields change.
const (
	packLabelW = 11
	packValueW = 16
)

func (m Model) renderPack() string {
	i := m.pack.info()
	n := len(m.pack.srcs)

	rows := []string{
		packRow(m, packFieldFormat, "Format", packChoice(i.Label), strings.Join(i.Tools, ", ")),
		packRow(m, packFieldLevel, "Level", packLevelValue(m.pack), packLevelHint(i)),
		packRow(m, packFieldPassword, "Password", m.packPasswordValue(), m.packPasswordHint()),
		packRow(m, packFieldName, "Name", m.pack.name.View(), ""),
	}

	lines := []string{
		ui.DialogTitle.Render(fmt.Sprintf("Pack %d %s", n, items(n))),
		"",
	}
	lines = append(lines, rows...)
	lines = append(lines, "", ui.Faint.Render("→ "+ui.TruncTail(m.pack.dest, contentWidth-2)))

	if m.pack.opts().Weak() {
		lines = append(lines, ui.Danger.Render("ZipCrypto is weak — install p7zip for AES-256"))
	}
	if m.pack.status != "" {
		lines = append(lines, "", ui.Danger.Render(ui.TruncTail(m.pack.status, contentWidth)))
	}
	lines = append(lines, "", ui.Faint.Render("↑↓ field · ←→ change · enter pack · esc cancel"))

	return ui.Dialog.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// packRow lays out one field: label, value, and a faint note about it.
func packRow(m Model, f packField, label, value, note string) string {
	name := "  " + ui.PadRight(label, packLabelW-2)
	if m.pack.focus == f {
		name = ui.Cursor.Render(ui.PadRight("▸ "+label, packLabelW))
	}
	row := name + value
	if note == "" {
		return row
	}
	gap := contentWidth - lipgloss.Width(row) - lipgloss.Width(note)
	if gap < 1 {
		gap = 1
	}
	return row + strings.Repeat(" ", gap) + ui.Faint.Render(note)
}

// packChoice renders a ←/→ field's current value.
func packChoice(value string) string {
	return "◂ " + ui.PadRight(value, packValueW-4) + "▸"
}

func packLevelValue(s packState) string {
	if !s.info().Levels() {
		return "  " + ui.PadRight("—", packValueW-2)
	}
	return packChoice(strconv.Itoa(s.level))
}

func packLevelHint(i fileops.PackFormatInfo) string {
	switch {
	case !i.Levels():
		return "no compression"
	case i.Format == fileops.PackZip || i.Format == fileops.Pack7z:
		// Only the containers with a per-entry method can leave data uncompressed.
		return fmt.Sprintf("0 store … %d smallest", i.MaxLevel)
	default:
		return fmt.Sprintf("%d fastest … %d smallest", i.MinLevel, i.MaxLevel)
	}
}

func (m Model) packPasswordValue() string {
	if !m.pack.canEncrypt() {
		return ui.Faint.Render(ui.PadRight("—", packValueW))
	}
	return ui.PadRight(m.pack.pw.View(), packValueW)
}

func (m Model) packPasswordHint() string {
	if !m.pack.canEncrypt() {
		return m.pack.info().Label + " cannot encrypt"
	}
	if label := m.pack.opts().EncryptionLabel(); label != "" {
		return label
	}
	switch m.pack.info().Encrypt {
	case "7z":
		return "AES-256 available"
	default:
		return "ZipCrypto only"
	}
}

func (m Model) renderUnpackPw() string {
	arc := "the archive"
	if len(m.unpackPw.job.Srcs) == 1 {
		arc = filepath.Base(m.unpackPw.job.Srcs[0])
	} else if n := len(m.unpackPw.job.Srcs); n > 1 {
		arc = fmt.Sprintf("%d archives", n)
	}

	title := ui.DialogTitle.Render("Password needed")
	if m.unpackPw.again {
		title = ui.Danger.Render("Wrong password")
	}
	lines := []string{
		title,
		"",
		ui.Faint.Render("for " + ui.TruncTail(arc, contentWidth-4)),
		ui.AddrEdit.Width(contentWidth).Render(m.unpackPw.input.View()),
		"",
		ui.DialogHint.Render("enter") + " unpack    " + ui.DialogHint.Render("esc") + " cancel",
	}
	return ui.Dialog.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

// packSummary is the one-line description of a pack job's settings, shown on
// the confirm prompt so nothing about it is a surprise.
func packSummary(o fileops.PackOpts) string {
	i := o.Format.Info()
	s := i.Label
	if i.Levels() {
		s += fmt.Sprintf(" · level %d", o.Level)
	}
	if label := o.EncryptionLabel(); label != "" {
		s += " · " + label
	}
	return s
}
