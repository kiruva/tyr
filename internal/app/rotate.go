package app

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/metastrip"
	"github.com/kiruva/tyr/internal/termsafe"
	"github.com/kiruva/tyr/internal/ui"
)

// The rotate dialog turns or flips the marked images, or the one under the
// cursor, by changing their EXIF orientation. One key is one turn, and it
// happens at once: nothing is lost by it, and ctrl+z turns them back.

// rotateState is the dialog while it is open.
type rotateState struct {
	srcs []string
	// now is the orientation of a single image, for the dialog to say how it
	// is shown; 0 when there are several, or it could not be read.
	now int
}

// rotateKeys are the dialog's choices, in the order it lists them.
var rotateKeys = []struct {
	label string
	keys  []string
	turn  metastrip.Turn
}{
	{"→ r", []string{"right", "r"}, metastrip.RotateRight},
	{"← l", []string{"left", "l"}, metastrip.RotateLeft},
	{"u", []string{"u"}, metastrip.Rotate180},
	{"h", []string{"h"}, metastrip.FlipHorizontal},
	{"v", []string{"v"}, metastrip.FlipVertical},
}

// rotateReadLimit is the largest single image the dialog reads to say how it
// is shown now; past it the question is skipped rather than stall the UI.
const rotateReadLimit = 32 << 20

// openRotate opens the dialog for the selection. Folders are taken whole —
// the job finds the images in them — but a file picked by name has to be an
// image.
func (m *Model) openRotate() {
	src := &m.panes[m.active]
	switch {
	case src.IsRemote():
		m.errText = "rotating works on local files only"
		return
	case src.InArchive():
		m.errText = "not available inside an archive — unpack it first"
		return
	}
	names := src.SelectedNames()
	if len(names) == 0 {
		return
	}
	srcs := make([]string, 0, len(names))
	for _, n := range names {
		p := filepath.Join(src.Path, n)
		if !isDirPath(p) && !fileops.IsImage(n) {
			m.errText = "not an image: " + n
			return
		}
		srcs = append(srcs, p)
	}
	s := rotateState{srcs: srcs}
	if len(srcs) == 1 && !isDirPath(srcs[0]) {
		s.now = orientationNow(srcs[0])
	}
	m.rotate = s
	m.mode = modeRotate
}

// orientationNow reads one image's orientation, 0 when it cannot.
func orientationNow(path string) int {
	info, err := os.Stat(path)
	if err != nil || info.Size() > rotateReadLimit {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	res, err := metastrip.Strip(data, metastrip.Options{})
	if err != nil {
		return 0
	}
	return max(res.Orientation, 1)
}

// onRotateKey runs the turn a key names, or closes the dialog.
func (m Model) onRotateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" || msg.String() == "q" {
		m.mode = modeNormal
		m.rotate = rotateState{}
		return m, nil
	}
	for _, c := range rotateKeys {
		for _, k := range c.keys {
			if msg.String() == k {
				m.pending = fileops.Job{Op: fileops.OpRotate, Srcs: m.rotate.srcs, Turn: c.turn}
				m.rotate = rotateState{}
				cmd := m.startPending()
				return m, cmd
			}
		}
	}
	return m, nil
}

func (m Model) renderRotate() string {
	s := m.rotate
	what := countLabel(len(s.srcs))
	if len(s.srcs) == 1 {
		what = filepath.Base(s.srcs[0])
	}
	lines := []string{
		ui.DialogTitle.Render("Rotate or flip"),
		"",
		ui.Faint.Render(ui.TruncTail(termsafe.Line(what), contentWidth)),
	}
	if s.now > 0 {
		lines = append(lines, ui.Faint.Render("shown now: "+metastrip.OrientationName(s.now)))
	}
	lines = append(lines, "")
	for _, c := range rotateKeys {
		lines = append(lines, ui.DialogHint.Render(ui.PadRight(c.label, 5))+" "+c.turn.String())
	}
	lines = append(lines, "",
		ui.Faint.Render("EXIF orientation only — the pixels are not touched"),
		ui.DialogHint.Render("esc")+" cancel"+ui.Faint.Render("    ctrl+z turns them back"))
	return ui.Dialog.Width(dialogWidth).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
