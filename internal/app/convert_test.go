package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
)

// selectForConvert marks an entry and opens the convert dialog on it.
func selectForConvert(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	m = focusEntry(t, m, name)
	m = press(t, m, "space")
	m = press(t, m, "a")
	if got := m.(Model).mode; got != modeConvert {
		t.Fatalf("mode = %v, want modeConvert (err %q)", got, m.(Model).errText)
	}
	return m
}

// haveFFmpeg skips unless ffmpeg is installed and actually runs: one whose
// libraries were upgraded out from under it is on PATH and useless.
func haveFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := fileops.AvailableAudioFormats(); err != nil {
		t.Skip(err)
	}
}

func arrow(t *testing.T, m tea.Model, k tea.KeyType) tea.Model {
	t.Helper()
	m, cmd := m.Update(tea.KeyMsg{Type: k})
	return drain(t, m, cmd)
}

// Something that is not audio never reaches the dialog.
func TestConvertRejectsNonAudio(t *testing.T) {
	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))
	m := focusEntry(t, twoPanes(t, dir, dest), "notes.txt")
	m = press(t, m, "space")
	m = press(t, m, "a")
	app := m.(Model)
	if app.mode == modeConvert || !strings.Contains(app.errText, "not an audio file") {
		t.Errorf("mode = %v, err = %q", app.mode, app.errText)
	}
}

// Changing the format puts the quality back at that format's default, and
// Enter goes on to the confirm prompt with the job described.
func TestConvertDialogToConfirm(t *testing.T) {
	haveFFmpeg(t)
	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "song.mp3"))
	m := selectForConvert(t, twoPanes(t, dir, dest), "song.mp3")

	m = press(t, m, "down")
	m = arrow(t, m, tea.KeyRight)
	app := m.(Model)
	if app.convert.info().HasQualities() && app.convert.quality == app.convert.info().DefQuality {
		t.Errorf("→ on the quality field did not change it")
	}
	m = press(t, m, "up")
	m = arrow(t, m, tea.KeyRight)
	app = m.(Model)
	if app.convert.quality != app.convert.info().DefQuality {
		t.Errorf("quality %d carried over to %s, want its default", app.convert.quality, app.convert.info().Label)
	}

	m = press(t, m, "enter")
	app = m.(Model)
	if app.mode != modeConfirm || app.pending.Op != fileops.OpConvert || app.pending.Dest != dest {
		t.Fatalf("mode = %v, pending = %+v", app.mode, app.pending)
	}
	if app.pending.Convert.Encoder == "" {
		t.Error("the job carries no encoder")
	}
}

// Converting into the same format in the same directory would read and replace
// one file at once; the dialog stays open and says so.
func TestConvertRefusesToOverwriteSource(t *testing.T) {
	haveFFmpeg(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "song.mp3"))
	m := selectForConvert(t, twoPanes(t, dir, dir), "song.mp3")

	app := m.(Model)
	for app.convert.info().Format != fileops.AudioMP3 {
		m = arrow(t, m, tea.KeyRight)
		app = m.(Model)
		if app.convert.sel == 0 {
			t.Skip("this ffmpeg cannot write mp3")
		}
	}
	m = press(t, m, "enter")
	app = m.(Model)
	if app.mode != modeConvert || !strings.Contains(app.convert.status, "over itself") {
		t.Errorf("mode = %v, status = %q", app.mode, app.convert.status)
	}
}

// The whole flow with the real ffmpeg: the output appears in the other pane,
// and undo takes it away again.
func TestConvertRunsAndUndoes(t *testing.T) {
	haveFFmpeg(t)
	dir, dest := t.TempDir(), t.TempDir()
	src := filepath.Join(dir, "tone.wav")
	gen := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1", src)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("cannot make a test tone: %v %s", err, out)
	}

	m := selectForConvert(t, twoPanes(t, dir, dest), "tone.wav")
	ext := m.(Model).convert.info().Ext
	m = press(t, m, "enter")
	m = press(t, m, "y")
	app := m.(Model)
	if app.errText != "" {
		t.Fatalf("convert failed: %s", app.errText)
	}
	out := filepath.Join(dest, "tone"+ext)
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("no output: %v", err)
	}

	m = press(t, m, "ctrl+z")
	_ = press(t, m, "y")
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("undo left the output behind: %v", err)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("undo touched the source: %v", err)
	}
}

// A folder is accepted as it stands; what is inside is the job's to find.
func TestConvertAcceptsFolder(t *testing.T) {
	haveFFmpeg(t)
	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "album", "01.mp3"))
	m := selectForConvert(t, twoPanes(t, dir, dest), "album")
	if got := convertExample(m.(Model).convert.srcs, m.(Model).convert.info()); !strings.HasPrefix(got, "album/ → album/…") {
		t.Errorf("example = %q", got)
	}
}

// The progress dialog shows a second bar, and the position, for a job that
// reports how far into the current file it is.
func TestProgressShowsPartBar(t *testing.T) {
	m := newInDir(t, t.TempDir()).(Model)
	m.mode = modeProgress
	m.pending = fileops.Job{Op: fileops.OpConvert}
	m.progress = fileops.Progress{Current: "album/01.mp3", Done: 1, Total: 4, Part: 0.5, PartLabel: "1:30 / 3:00"}
	view := m.renderProgress()
	if !strings.Contains(view, "1:30 / 3:00") || !strings.Contains(view, "1 / 4") {
		t.Errorf("progress view lacks the file position:\n%s", view)
	}
	m.progress.PartLabel = ""
	if strings.Contains(m.renderProgress(), "1:30") {
		t.Error("a job with no position still shows one")
	}
}
