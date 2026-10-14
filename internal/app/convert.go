package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/ui"
)

// The convert dialog picks a format and a quality for turning the selected
// audio files into another format, one output per source, in the other pane.
// Like packing, Enter goes on to the confirm prompt rather than straight to
// work. Only the formats the installed ffmpeg has an encoder for are offered.

type convertField int

const (
	convertFieldFormat convertField = iota
	convertFieldQuality
	convertFieldCount
)

// convertState is the dialog while it is open.
type convertState struct {
	formats []fileops.AudioFormatInfo
	sel     int // index into formats
	quality int // index into the selected format's Qualities
	focus   convertField
	status  string

	srcs []string // absolute paths to convert
	dest string   // directory the outputs are written to
}

// info is the currently selected format.
func (s convertState) info() fileops.AudioFormatInfo {
	if s.sel < 0 || s.sel >= len(s.formats) {
		return fileops.AudioFormatInfo{}
	}
	return s.formats[s.sel]
}

func (s convertState) opts() fileops.ConvertOpts {
	return fileops.ConvertOptsFor(s.info(), s.quality)
}

// openConvert opens the dialog for the active pane's selection. Conversion is
// local work on real files: ffmpeg cannot reach into an archive or across ssh.
func (m *Model) openConvert() {
	src, dst := &m.panes[m.active], &m.panes[1-m.active]
	switch {
	case src.IsRemote() || dst.IsRemote():
		m.errText = "audio conversion works on local files only"
		return
	case src.InArchive():
		m.errText = "not available inside an archive — unpack it first"
		return
	case dst.InArchive():
		m.errText = "destination pane is inside an archive"
		return
	}
	names := src.SelectedNames()
	if len(names) == 0 {
		return
	}
	// A folder is taken whole — the job finds the audio in it, off the UI
	// thread — but a file picked by name has to be audio itself.
	srcs := make([]string, 0, len(names))
	for _, n := range names {
		p := filepath.Join(src.Path, n)
		if !isDirPath(p) && !fileops.IsAudio(n) {
			m.errText = "not an audio file: " + n
			return
		}
		srcs = append(srcs, p)
	}

	formats, err := fileops.AvailableAudioFormats()
	switch {
	case errors.Is(err, fileops.ErrNoFFmpeg):
		m.errText = "ffmpeg is not installed — press C to see what tyr needs"
		return
	case err != nil:
		m.errText = err.Error()
		return
	case len(formats) == 0:
		m.errText = "this ffmpeg has no encoder for any format tyr writes"
		return
	}

	m.convert = convertState{formats: formats, srcs: srcs, dest: dst.Path}
	m.convert.quality = m.convert.info().DefQuality
	m.mode = modeConvert
}

// onConvertKey drives the dialog.
func (m Model) onConvertKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.convert.status = ""
	switch msg.String() {
	case "esc":
		m.closeConvert()
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.commitConvert()
	case "tab", "down", "shift+tab", "up":
		// Two fields, so either direction is the other one — unless the format
		// has no quality to choose, in which case there is nowhere to go.
		if m.convert.info().HasQualities() {
			m.convert.focus = (m.convert.focus + 1) % convertFieldCount
		}
	case "left", "right":
		step := 1
		if msg.String() == "left" {
			step = -1
		}
		if m.convert.focus == convertFieldFormat {
			m.cycleConvertFormat(step)
		} else {
			m.cycleConvertQuality(step)
		}
	}
	return m, nil
}

// cycleConvertFormat moves to the next available format, at that format's
// default quality: a q5 means nothing to an mp3.
func (m *Model) cycleConvertFormat(step int) {
	n := len(m.convert.formats)
	if n == 0 {
		return
	}
	m.convert.sel = (m.convert.sel + step + n) % n
	m.convert.quality = m.convert.info().DefQuality
	if !m.convert.info().HasQualities() {
		m.convert.focus = convertFieldFormat
	}
}

func (m *Model) cycleConvertQuality(step int) {
	n := len(m.convert.info().Qualities)
	if n < 2 {
		return
	}
	m.convert.quality = (m.convert.quality + step + n) % n
}

// commitConvert hands the job to the confirm prompt. A source that would be
// written over itself — an mp3 converted to mp3 in its own directory — keeps
// the dialog open with the reason, since ffmpeg cannot read and replace one
// file at once.
func (m Model) commitConvert() (tea.Model, tea.Cmd) {
	f := m.convert.info().Format
	for _, src := range m.convert.srcs {
		// Inside a folder, a file already in the format is skipped by the job;
		// only one picked by name is worth stopping for.
		if isDirPath(src) {
			continue
		}
		if fileops.OverwritesSource(src, fileops.ConvertTarget(src, m.convert.dest, f)) {
			m.convert.status = filepath.Base(src) + " would be written over itself"
			return m, nil
		}
	}

	m.pending = fileops.Job{
		Op:      fileops.OpConvert,
		Srcs:    m.convert.srcs,
		Dest:    m.convert.dest,
		Convert: m.convert.opts(),
	}
	// A name already taken is asked about file by file as the job reaches it,
	// the same as a copy.
	m.willOverwrite = false
	m.closeConvert()
	m.mode = modeConfirm
	return m, nil
}

func isDirPath(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func (m *Model) closeConvert() {
	m.mode = modeNormal
	m.convert = convertState{}
}

// renderConvert lays the dialog out on the same grid as the pack dialog.
func (m Model) renderConvert() string {
	i := m.convert.info()
	n := len(m.convert.srcs)

	quality := "  " + ui.PadRight("—", packValueW-2)
	if i.HasQualities() {
		quality = packChoice(i.Qualities[m.convert.quality].Label)
	} else if len(i.Qualities) == 1 {
		quality = "  " + ui.PadRight(i.Qualities[0].Label, packValueW-2)
	}

	lines := []string{
		ui.DialogTitle.Render(fmt.Sprintf("Convert %d %s", n, items(n))),
		"",
		m.convertRow(convertFieldFormat, "Format", packChoice(i.Label), i.Encoder),
		m.convertRow(convertFieldQuality, "Quality", quality, ""),
		"",
		ui.Faint.Render("→ " + ui.TruncTail(m.convert.dest, contentWidth-2)),
		ui.Faint.Render(ui.TruncTail(convertExample(m.convert.srcs, i), contentWidth)),
	}
	if i.Experimental {
		lines = append(lines, ui.Danger.Render(ui.TruncTail(
			"experimental encoder — lib"+i.Encoder+" sounds better", contentWidth)))
	}
	if m.convert.status != "" {
		lines = append(lines, "", ui.Danger.Render(ui.TruncTail(m.convert.status, contentWidth)))
	}
	lines = append(lines, "", ui.Faint.Render("↑↓ field · ←→ change · enter convert · esc cancel"))
	return ui.Dialog.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func (m Model) convertRow(f convertField, label, value, note string) string {
	name := "  " + ui.PadRight(label, packLabelW-2)
	if m.convert.focus == f {
		name = ui.Cursor.Render(ui.PadRight("▸ "+label, packLabelW))
	}
	row := name + value
	if note == "" {
		return row
	}
	gap := max(contentWidth-lipgloss.Width(row)-lipgloss.Width(note), 1)
	return row + strings.Repeat(" ", gap) + ui.Faint.Render(note)
}

// convertExample shows what the first file will be called, which is the whole
// of the naming rule; a folder keeps its name and its shape.
func convertExample(srcs []string, i fileops.AudioFormatInfo) string {
	if len(srcs) == 0 {
		return ""
	}
	first := filepath.Base(srcs[0])
	out := filepath.Base(fileops.ConvertTarget(srcs[0], "", i.Format))
	if isDirPath(srcs[0]) {
		first, out = first+"/", first+"/…"+i.Ext
	}
	s := first + " → " + out
	if len(srcs) > 1 {
		s += fmt.Sprintf(" (+%d)", len(srcs)-1)
	}
	return s
}
