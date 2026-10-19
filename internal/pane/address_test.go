package pane

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// typeAddr feeds a string into the focused address bar one rune at a time.
func typeAddr(m *Model, s string) {
	for _, r := range s {
		m.UpdateAddr(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestAddressBarJump(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "child")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := New(root)
	m.SetSize(40, 20)

	m.BeginEditPath()
	if !m.EditingPath() {
		t.Fatal("address bar should be focused")
	}
	typeAddr(&m, string(os.PathSeparator)+"child")
	if err := m.CommitEditPath(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if m.Path != sub {
		t.Fatalf("Path = %q, want %q", m.Path, sub)
	}
	if m.EditingPath() {
		t.Fatal("address bar should be blurred after commit")
	}
	if got := m.addr.Value(); got != sub {
		t.Fatalf("address bar = %q, want %q", got, sub)
	}
}

func TestAddressBarRejectsNonDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "note.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := New(root)
	m.BeginEditPath()
	typeAddr(&m, string(os.PathSeparator)+"note.txt")
	if err := m.CommitEditPath(); err == nil {
		t.Fatal("expected an error for a plain file")
	}
	if !m.EditingPath() {
		t.Fatal("edit should stay open after a failed commit")
	}
	if m.Path != root {
		t.Fatalf("Path changed to %q", m.Path)
	}
}

func TestAddressBarTracksNavigation(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "child")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := New(root)
	for i, e := range m.Entries {
		if e.Name == "child" {
			m.Cursor = i
		}
	}
	m.Enter()
	if m.Path != sub {
		t.Fatalf("Path = %q, want %q", m.Path, sub)
	}
	if got := m.addr.Value(); got != sub {
		t.Fatalf("address bar = %q, want %q", got, sub)
	}

	m.Ascend()
	if got := m.addr.Value(); got != root {
		t.Fatalf("address bar = %q after ascend, want %q", got, root)
	}
}

func TestAddressBarCancelRestores(t *testing.T) {
	root := t.TempDir()
	m := New(root)

	m.BeginEditPath()
	typeAddr(&m, "/nowhere")
	m.CancelEditPath()

	if m.EditingPath() {
		t.Fatal("still editing after cancel")
	}
	if got := m.addr.Value(); got != root {
		t.Fatalf("address bar = %q, want %q", got, root)
	}
}

func TestAddressBarExpandsTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	m := New(t.TempDir())
	got, err := m.resolve("~")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(home) {
		t.Fatalf("resolve(~) = %q, want %q", got, home)
	}
}

func TestAddressBarSuggestsDirectories(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"alpha", "album", "beta"} {
		if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m := New(root)
	m.BeginEditPath()
	typeAddr(&m, string(os.PathSeparator)+"al")

	sugg := m.addr.AvailableSuggestions()
	want := []string{
		filepath.Join(root, "album") + string(os.PathSeparator),
		filepath.Join(root, "alpha") + string(os.PathSeparator),
	}
	if len(sugg) != len(want) {
		t.Fatalf("suggestions = %v, want %v", sugg, want)
	}
	for i := range want {
		if sugg[i] != want[i] {
			t.Fatalf("suggestions = %v, want %v", sugg, want)
		}
	}
}

// Completion reads a folder once while names in it are typed, and not at all
// for a message that changes nothing (the cursor blinking).
func TestAddressBarReadsAFolderOnce(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"alpha", "alps"} {
		if err := os.Mkdir(filepath.Join(root, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := New(root)
	m.BeginEditPath()
	typeAddr(&m, string(os.PathSeparator)+"a")
	listed := m.addrList
	if len(listed) != 2 {
		t.Fatalf("listed %d entries, want the two folders", len(listed))
	}

	// Gone from disk now: a re-read would find nothing to suggest.
	if err := os.Remove(filepath.Join(root, "alps")); err != nil {
		t.Fatal(err)
	}
	typeAddr(&m, "l")
	m.UpdateAddr(struct{}{})
	if len(m.addrList) != 2 || &m.addrList[0] != &listed[0] {
		t.Error("the folder was read again for a name typed in it")
	}
}
