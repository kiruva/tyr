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
	"github.com/kiruva/tyr/internal/metastrip"
	"github.com/kiruva/tyr/internal/ui"
)

// The convert dialog turns the selection into something else in the other
// pane, one output per source. What that means depends on what is selected:
// audio gets a format and a quality and goes through ffmpeg; an image gets a
// clean copy of itself, metadata left behind. A selection of both shows both
// sections, and a folder, whose contents the job finds, shows both too. Like
// packing, Enter goes on to the confirm prompt rather than straight to work.
// Only the formats the installed ffmpeg has an encoder for are offered.

type convertField int

const (
	convertFieldFormat convertField = iota
	convertFieldQuality
	convertFieldOrient
	convertFieldICC
)

// convertState is the dialog while it is open.
type convertState struct {
	// audio is whether the audio section can convert anything; audioNote says
	// why not when the selection has audio but ffmpeg cannot help.
	audio     bool
	audioNote string
	formats   []fileops.AudioFormatInfo
	sel       int // index into formats
	quality   int // index into the selected format's Qualities

	images     bool
	keepOrient bool
	keepICC    bool

	focus  convertField
	status string

	srcs []string // absolute paths to convert
	dest string   // directory the outputs are written to
	// inPlace is set when the other pane is the same directory: images are
	// then stripped where they are, which nothing can undo.
	inPlace bool
}

// info is the currently selected format.
func (s convertState) info() fileops.AudioFormatInfo {
	if !s.audio || s.sel < 0 || s.sel >= len(s.formats) {
		return fileops.AudioFormatInfo{}
	}
	return s.formats[s.sel]
}

func (s convertState) opts() fileops.ConvertOpts {
	var o fileops.ConvertOpts
	if s.audio {
		o = fileops.ConvertOptsFor(s.info(), s.quality)
	}
	o.Images = s.images
	o.Strip = metastrip.Options{KeepOrientation: s.keepOrient, KeepICC: s.keepICC}
	return o
}

// fields is the rows that can take the cursor, in order.
func (s convertState) fields() []convertField {
	var out []convertField
	if s.audio {
		out = append(out, convertFieldFormat)
		if s.info().HasQualities() {
			out = append(out, convertFieldQuality)
		}
	}
	if s.images {
		out = append(out, convertFieldOrient, convertFieldICC)
	}
	return out
}

// openConvert opens the dialog for the active pane's selection. Conversion is
// local work on real files: ffmpeg cannot reach into an archive or across ssh.
func (m *Model) openConvert() {
	src, dst := &m.panes[m.active], &m.panes[1-m.active]
	switch {
	case src.IsRemote() || dst.IsRemote():
		m.errText = "conversion works on local files only"
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
	// A folder is taken whole — the job finds the audio and images in it, off
	// the UI thread — but a file picked by name has to be one or the other.
	var hasAudio, hasImages, onlyAudio = false, false, true
	srcs := make([]string, 0, len(names))
	for _, n := range names {
		p := filepath.Join(src.Path, n)
		switch {
		case isDirPath(p):
			hasAudio, hasImages, onlyAudio = true, true, false
		case fileops.IsAudio(n):
			hasAudio = true
		case fileops.IsImage(n):
			hasImages, onlyAudio = true, false
		default:
			m.errText = "not audio or an image: " + n
			return
		}
		srcs = append(srcs, p)
	}

	s := convertState{
		images: hasImages, keepOrient: true,
		srcs: srcs, dest: dst.Path, inPlace: sameDir(src.Path, dst.Path),
	}
	if hasAudio {
		formats, err := fileops.AvailableAudioFormats()
		var reason string
		switch {
		case errors.Is(err, fileops.ErrNoFFmpeg):
			reason = "ffmpeg is not installed"
		case err != nil:
			reason = err.Error()
		case len(formats) == 0:
			reason = "this ffmpeg has no encoder for any format tyr writes"
		}
		if reason != "" && onlyAudio {
			m.errText = reason + " — " + m.capsHint()
			return
		}
		s.audio, s.audioNote, s.formats = reason == "", reason, formats
		s.quality = s.info().DefQuality
	}
	if f := s.fields(); len(f) > 0 {
		s.focus = f[0]
	}
	m.convert = s
	m.mode = modeConvert
}

// capsHint is where to look when a tool is missing.
func (m Model) capsHint() string {
	if k := m.keys.Caps.Help().Key; k != "" {
		return "press " + k + " to see what tyr needs"
	}
	return "press " + m.keys.Help.Help().Key + " then tab to see what tyr needs"
}

// sameDir reports whether two paths are one directory, links and case
// folding included.
func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	x, err := os.Stat(a)
	if err != nil {
		return false
	}
	y, err := os.Stat(b)
	return err == nil && os.SameFile(x, y)
}

// onConvertKey drives the dialog.
func (m Model) onConvertKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.convert.status = ""
	switch msg.String() {
	case "esc":
		m.closeConvert()
	case "enter":
		return m.commitConvert()
	case "tab", "down":
		m.moveConvertFocus(1)
	case "shift+tab", "up":
		m.moveConvertFocus(-1)
	case "left", "right", " ":
		step := 1
		if msg.String() == "left" {
			step = -1
		}
		switch m.convert.focus {
		case convertFieldFormat:
			m.cycleConvertFormat(step)
		case convertFieldQuality:
			m.cycleConvertQuality(step)
		case convertFieldOrient:
			m.convert.keepOrient = !m.convert.keepOrient
		case convertFieldICC:
			m.convert.keepICC = !m.convert.keepICC
		}
	}
	return m, nil
}

func (m *Model) moveConvertFocus(step int) {
	f := m.convert.fields()
	if len(f) == 0 {
		return
	}
	at := 0
	for i, x := range f {
		if x == m.convert.focus {
			at = i
		}
	}
	m.convert.focus = f[(at+step+len(f))%len(f)]
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
}

func (m *Model) cycleConvertQuality(step int) {
	n := len(m.convert.info().Qualities)
	if n < 2 {
		return
	}
	m.convert.quality = (m.convert.quality + step + n) % n
}

// commitConvert hands the job to the confirm prompt. An audio file that would
// be written over itself — an mp3 converted to mp3 in its own directory —
// keeps the dialog open with the reason, since ffmpeg cannot read and replace
// one file at once. An image in the same place is different: stripping it in
// place is the point, and the confirm prompt says what that costs.
func (m Model) commitConvert() (tea.Model, tea.Cmd) {
	if m.convert.audio {
		f := m.convert.info().Format
		for _, src := range m.convert.srcs {
			// Inside a folder, a file already in the format is skipped by the
			// job; only one picked by name is worth stopping for.
			if isDirPath(src) || !fileops.IsAudio(src) {
				continue
			}
			if fileops.OverwritesSource(src, fileops.ConvertTarget(src, m.convert.dest, f)) {
				m.convert.status = filepath.Base(src) + " would be written over itself"
				return m, nil
			}
		}
	}
	if !m.convert.audio && !m.convert.images {
		m.convert.status = "nothing here can be converted: " + m.convert.audioNote
		return m, nil
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
	m.convertInPlace = m.convert.inPlace && m.convert.images
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
	s := m.convert
	n := len(s.srcs)
	lines := []string{ui.DialogTitle.Render(fmt.Sprintf("Convert %d %s", n, items(n)))}

	switch {
	case s.audio:
		i := s.info()
		quality := "  " + ui.PadRight("—", packValueW-2)
		if i.HasQualities() {
			quality = packChoice(i.Qualities[s.quality].Label)
		} else if len(i.Qualities) == 1 {
			quality = "  " + ui.PadRight(i.Qualities[0].Label, packValueW-2)
		}
		lines = append(lines, "", ui.Faint.Render("Audio"),
			m.convertRow(convertFieldFormat, "Format", packChoice(i.Label), i.Encoder),
			m.convertRow(convertFieldQuality, "Quality", quality, ""),
		)
		if i.Experimental {
			lines = append(lines, ui.Danger.Render(ui.TruncTail(
				"experimental encoder — lib"+i.Encoder+" sounds better", contentWidth)))
		}
	case s.audioNote != "":
		lines = append(lines, "", ui.Faint.Render("Audio"),
			ui.Danger.Render(ui.TruncTail(s.audioNote+" — audio is skipped", contentWidth)))
	}
	if s.images {
		lines = append(lines, "", ui.Faint.Render("Images · clean copy, metadata removed"),
			m.convertRow(convertFieldOrient, "Rotation", packChoice(keepLabel(s.keepOrient)), "EXIF orientation"),
			m.convertRow(convertFieldICC, "Colour", packChoice(keepLabel(s.keepICC)), "ICC profile"),
		)
	}

	lines = append(lines, "",
		ui.Faint.Render("→ "+ui.TruncTail(s.dest, contentWidth-2)),
		ui.Faint.Render(ui.TruncTail(convertExample(s.srcs, s.info(), s.audio), contentWidth)),
	)
	if s.inPlace && s.images {
		lines = append(lines, ui.Danger.Render(ui.TruncTail(
			"same folder both sides — images are stripped in place", contentWidth)))
	}
	if s.status != "" {
		lines = append(lines, "", ui.Danger.Render(ui.TruncTail(s.status, contentWidth)))
	}
	lines = append(lines, "", ui.Faint.Render("↑↓ field · ←→ change · enter convert · esc cancel"))
	return ui.Dialog.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}

func keepLabel(keep bool) string {
	if keep {
		return "keep"
	}
	return "remove"
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
// of the naming rule; a folder keeps its name and its shape, and an image
// keeps its name outright.
func convertExample(srcs []string, i fileops.AudioFormatInfo, audio bool) string {
	if len(srcs) == 0 {
		return ""
	}
	first := filepath.Base(srcs[0])
	out := first
	switch {
	case isDirPath(srcs[0]):
		first, out = first+"/", first+"/…"
		if audio {
			out += i.Ext
		}
	case fileops.IsAudio(first) && audio:
		out = filepath.Base(fileops.ConvertTarget(srcs[0], "", i.Format))
	}
	s := first + " → " + out
	if len(srcs) > 1 {
		s += fmt.Sprintf(" (+%d)", len(srcs)-1)
	}
	return s
}
