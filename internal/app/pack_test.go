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

// twoPanes puts the panes in two different directories, which is what every
// archive operation needs: source in one, destination in the other.
func twoPanes(t *testing.T, left, right string) tea.Model {
	t.Helper()
	m := newInDir(t, left).(Model)
	m.panes[0].Path = left
	m.panes[1].Path = right
	m.panes[0].Refresh()
	m.panes[1].Refresh()
	return m
}

func haveTool(t *testing.T, bins ...string) {
	t.Helper()
	for _, b := range bins {
		if _, err := exec.LookPath(b); err != nil {
			t.Skipf("%s is not installed", b)
		}
	}
}

// selectForPack focuses an entry and marks it, then opens the pack dialog.
func selectForPack(t *testing.T, m tea.Model, name string) tea.Model {
	t.Helper()
	m = focusEntry(t, m, name)
	m = press(t, m, "space")
	m = press(t, m, "p")
	if got := m.(Model).mode; got != modePack {
		t.Fatalf("mode = %v, want modePack", got)
	}
	return m
}

// The dialog opens on the format tyr used to pack with, names the output after
// the selection, and offers only what the machine can actually create.
func TestPackDialogDefaults(t *testing.T) {
	haveTool(t, "tar", "gzip")

	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))
	m := selectForPack(t, twoPanes(t, dir, dest), "notes.txt")

	app := m.(Model)
	if got := app.pack.info().Label; got != "tar.gz" {
		t.Errorf("format = %q, want tar.gz", got)
	}
	if got := app.pack.name.Value(); got != "notes.txt.tar.gz" {
		t.Errorf("name = %q, want notes.txt.tar.gz", got)
	}
	if got := app.pack.level; got != 6 {
		t.Errorf("level = %d, want the gzip default 6", got)
	}
	for _, f := range app.pack.formats {
		for _, tool := range f.Tools {
			if _, err := exec.LookPath(tool); err != nil {
				t.Errorf("format %s is offered but %s is missing", f.Label, tool)
			}
		}
	}
}

// ←/→ change the highlighted setting; the extension follows the format, and the
// level is the new format's own default rather than a number out of its range.
func TestPackDialogCyclesFormatAndLevel(t *testing.T) {
	haveTool(t, "tar", "gzip", "zip")

	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))
	m := selectForPack(t, twoPanes(t, dir, dest), "notes.txt")

	// Walk the formats until zip comes up, however many are installed.
	for i := 0; i < 10 && m.(Model).pack.info().Format != fileops.PackZip; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	app := m.(Model)
	if app.pack.info().Format != fileops.PackZip {
		t.Fatal("never reached the zip format")
	}
	if got := app.pack.name.Value(); got != "notes.txt.zip" {
		t.Errorf("name after switching format = %q, want notes.txt.zip", got)
	}

	// Down to the level field, then one step down from the default.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.(Model).pack.focus; got != packFieldLevel {
		t.Fatalf("focus = %v, want the level field", got)
	}
	before := m.(Model).pack.level
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if got := m.(Model).pack.level; got != before-1 {
		t.Errorf("level = %d, want %d", got, before-1)
	}
}

// A name typed by hand is the user's; changing the format must not rewrite it.
func TestPackDialogKeepsTypedName(t *testing.T) {
	haveTool(t, "tar", "gzip")

	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))
	m := selectForPack(t, twoPanes(t, dir, dest), "notes.txt")

	// Down to the name field (the password is skipped for tar.gz) and edit it.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := m.(Model).pack.focus; got != packFieldName {
		t.Fatalf("focus = %v, want the name field", got)
	}
	m = typeKeys(t, m, "-v2")
	if got := m.(Model).pack.name.Value(); got != "notes.txt.tar.gz-v2" {
		t.Fatalf("typed name = %q", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})

	if got := m.(Model).pack.name.Value(); got != "notes.txt.tar.gz-v2" {
		t.Errorf("name after changing the format = %q, want the typed name kept", got)
	}
}

// The tar family cannot be encrypted, so the field says so and the cursor skips
// it rather than collecting a password that would be silently dropped.
func TestPackDialogSkipsPasswordForTar(t *testing.T) {
	haveTool(t, "tar", "gzip")

	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))
	m := selectForPack(t, twoPanes(t, dir, dest), "notes.txt")

	if m.(Model).pack.canEncrypt() {
		t.Fatal("tar.gz should not offer encryption")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // level
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown}) // would be password
	if got := m.(Model).pack.focus; got == packFieldPassword {
		t.Error("focus stopped on the password field for a format that cannot encrypt")
	}
}

// Enter goes to the confirm prompt with the settings spelled out, and y runs it.
func TestPackFlowCreatesArchive(t *testing.T) {
	haveTool(t, "tar", "gzip")

	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))
	m := selectForPack(t, twoPanes(t, dir, dest), "notes.txt")

	m = press(t, m, "enter")
	app := m.(Model)
	if app.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", app.mode)
	}
	if got := app.pending.Pack.Level; got != 6 {
		t.Errorf("pending level = %d, want 6", got)
	}
	if view := app.View(); !strings.Contains(view, "tar.gz · level 6") {
		t.Errorf("confirm prompt does not state the settings:\n%s", view)
	}

	m = press(t, m, "y")
	if got := m.(Model).mode; got != modeNormal {
		t.Fatalf("mode after packing = %v, want modeNormal", got)
	}
	if err := m.(Model).errText; err != "" {
		t.Fatalf("errText = %q", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "notes.txt.tar.gz")); err != nil {
		t.Fatalf("archive was not created: %v", err)
	}
}

// The whole password story, end to end: pack an encrypted archive from the
// dialog, then unpack it — the wrong password asks again, the right one works.
func TestPackEncryptedThenUnpackAsksForPassword(t *testing.T) {
	haveTool(t, "zip", "unzip")

	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))
	m := selectForPack(t, twoPanes(t, dir, dest), "notes.txt")

	for i := 0; i < 10 && m.(Model).pack.info().Format != fileops.PackZip; i++ {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	}
	if m.(Model).pack.info().Format != fileops.PackZip {
		t.Skip("this machine cannot create a .zip")
	}

	app := m.(Model)
	app.pack.focus = packFieldPassword
	app.pack.pw.SetValue("s3cr3t")
	m = app
	m = press(t, m, "enter")
	if got := m.(Model).pending.Pack.Password; got != "s3cr3t" {
		t.Fatalf("pending password = %q", got)
	}
	m = press(t, m, "y")
	if err := m.(Model).errText; err != "" {
		t.Fatalf("packing failed: %s", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "notes.txt.zip")); err != nil {
		t.Fatalf("encrypted archive was not created: %v", err)
	}

	// Unpack it back the other way: the destination pane becomes the source.
	back := t.TempDir()
	m2 := twoPanes(t, dest, back)
	m2 = focusEntry(t, m2, "notes.txt.zip")
	m2 = press(t, m2, "space")
	m2 = press(t, m2, "u")
	m2 = press(t, m2, "y")

	if got := m2.(Model).mode; got != modeUnpackPw {
		t.Fatalf("mode = %v, want the password prompt", got)
	}
	if m2.(Model).unpackPw.again {
		t.Error("the first prompt should not claim the password was wrong")
	}

	// A wrong password comes back to the prompt, saying so.
	app2 := m2.(Model)
	app2.unpackPw.input.SetValue("nope")
	m2 = app2
	m2 = press(t, m2, "enter")
	if got := m2.(Model).mode; got != modeUnpackPw {
		t.Fatalf("mode after a wrong password = %v, want the prompt again", got)
	}
	if !m2.(Model).unpackPw.again {
		t.Error("the second prompt should say the password was wrong")
	}

	app2 = m2.(Model)
	app2.unpackPw.input.SetValue("s3cr3t")
	m2 = app2
	m2 = press(t, m2, "enter")

	if got := m2.(Model).mode; got != modeNormal {
		t.Fatalf("mode after the right password = %v, want modeNormal", got)
	}
	if err := m2.(Model).errText; err != "" {
		t.Fatalf("errText = %q", err)
	}
	if _, err := os.Stat(filepath.Join(back, "notes.txt")); err != nil {
		t.Fatalf("file was not extracted: %v", err)
	}
}

// Esc at the prompt abandons the unpack, and the password goes with it.
func TestUnpackPasswordPromptCancels(t *testing.T) {
	haveTool(t, "zip", "unzip")

	dir, dest := t.TempDir(), t.TempDir()
	write(t, filepath.Join(dir, "notes.txt"))

	archive := filepath.Join(dir, "locked.zip")
	cmd := exec.Command("zip", "-P", "s3cr3t", archive, "notes.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("zip: %v\n%s", err, out)
	}

	m := twoPanes(t, dir, dest)
	m = focusEntry(t, m, "locked.zip")
	m = press(t, m, "space")
	m = press(t, m, "u")
	m = press(t, m, "y")
	if got := m.(Model).mode; got != modeUnpackPw {
		t.Fatalf("mode = %v, want the password prompt", got)
	}

	m = press(t, m, "esc")
	app := m.(Model)
	if app.mode != modeNormal {
		t.Errorf("mode = %v, want modeNormal", app.mode)
	}
	if app.unpackPw.input.Value() != "" || app.unpackPw.job.Op != 0 {
		t.Error("the cancelled prompt kept the job or the password")
	}
	if app.noticeText == "" {
		t.Error("cancelling should say what happened")
	}
}
