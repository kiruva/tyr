package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/fileops"
	"github.com/kiruva/tyr/internal/pane"
	"github.com/kiruva/tyr/internal/remote"
)

var testListing = remote.Listing{
	Dir: "/srv",
	Entries: []remote.Entry{
		{Name: "app.log", Size: 120},
		{Name: "www", IsDir: true},
	},
}

// cursorTo puts the pane's cursor on the named entry.
func cursorTo(t *testing.T, m *Model, idx int, name string) {
	t.Helper()
	for i, e := range m.panes[idx].Entries {
		if e.Name == name {
			m.panes[idx].Cursor = i
			return
		}
	}
	t.Fatalf("pane %d has no entry %q", idx, name)
}

// remotePane puts pane idx on a host with a listing already applied, without
// touching the network.
func remotePane(t *testing.T, m *Model, idx int, h remote.Host, dir string) {
	t.Helper()
	p := &m.panes[idx]
	p.OpenRemote(h, dir)
	l := testListing
	l.Dir = dir
	if !p.SetRemoteListing(dir, l) {
		t.Fatal("listing was not applied")
	}
}

// TestAddressBarOpensConnectionModal pins the routing: a remote target typed
// into the address bar is not dialled behind the user's back, it goes through the
// connection modal so a password or host key can be collected.
func TestAddressBarOpensConnectionModal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = typeKeys(t, m, "ssh://kim@box/srv")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.mode != modeConn {
		t.Fatalf("mode = %v, want the connection modal", app.mode)
	}
	if !app.conn.adHoc {
		t.Fatal("an address-bar target is not a saved connection")
	}
	if got := connHost(app.conn.target); got != (remote.Host{User: "kim", Name: "box"}) {
		t.Fatalf("target host = %+v", got)
	}
	if app.conn.target.Path != "/srv" {
		t.Fatalf("target path = %q", app.conn.target.Path)
	}
	if app.panes[0].IsRemote() {
		t.Fatal("the pane must not go remote before the connection succeeds")
	}
	if cmd == nil {
		t.Fatal("a connection attempt should have been started")
	}
}

func TestRemoteListingAppliesAndStaleIsIgnored(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	h := remote.Host{Name: "box"}
	app := m.(Model)
	app.panes[0].OpenRemote(h, "/srv")
	m = app

	m, _ = m.Update(remoteListMsg{pane: 0, host: h, path: "/srv", listing: testListing})
	app = m.(Model)
	if app.panes[0].Loading() {
		t.Fatal("listing should have cleared the loading flag")
	}
	names := []string{}
	for _, e := range app.panes[0].Entries {
		names = append(names, e.Name)
	}
	// ".." plus the two listed entries.
	if len(names) != 3 || names[0] != ".." {
		t.Fatalf("entries = %v", names)
	}

	// A reply for a directory the pane already left must not overwrite it.
	stale := remote.Listing{Dir: "/old", Entries: []remote.Entry{{Name: "ghost"}}}
	m, _ = m.Update(remoteListMsg{pane: 0, host: h, path: "/old", listing: stale})
	app = m.(Model)
	if app.panes[0].Path != "/srv" {
		t.Fatalf("stale listing moved the pane to %q", app.panes[0].Path)
	}
}

func TestRemoteListingErrorRestoresPane(t *testing.T) {
	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	h := remote.Host{Name: "box"}
	app := m.(Model)
	remotePane(t, &app, 0, h, "/srv")
	app.panes[0].GoRemote("/nope")
	m = app

	m, _ = m.Update(remoteListMsg{
		pane: 0, host: h, path: "/nope", previous: "/srv",
		err: errTest("no such directory"),
	})
	app = m.(Model)
	if app.panes[0].Path != "/srv" {
		t.Fatalf("pane should be back at /srv, got %q", app.panes[0].Path)
	}
	if app.panes[0].Loading() {
		t.Fatal("loading should be cleared after a failure")
	}
	if !strings.Contains(app.errText, "no such directory") {
		t.Fatalf("errText = %q", app.errText)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func TestRemoteJobAssembly(t *testing.T) {
	box := remote.Host{Name: "box"}
	other := remote.Host{Name: "other"}

	t.Run("download", func(t *testing.T) {
		m := New()
		m.width, m.height = 100, 24
		remotePane(t, &m, 0, box, "/srv")
		m.active = 0
		m.panes[1].Path = "/local/dir"

		cursorTo(t, &m, 0, "app.log")
		m.beginOp(fileops.OpCopy)

		if m.pending.Op != fileops.OpDownload {
			t.Fatalf("op = %v", m.pending.Op)
		}
		if m.pending.Host != box || m.pending.Dest != "/local/dir" {
			t.Fatalf("job = %+v", m.pending)
		}
		if len(m.pending.Srcs) != 1 || m.pending.Srcs[0] != "/srv/app.log" {
			t.Fatalf("srcs = %v", m.pending.Srcs)
		}
		if m.pending.Move {
			t.Fatal("copy should not set Move")
		}
		if m.mode != modeConfirm {
			t.Fatal("should be awaiting confirmation")
		}
	})

	t.Run("upload moves", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		m := New()
		remotePane(t, &m, 1, box, "/srv")
		m.active = 0
		m.panes[0] = pane.New(dir)
		for i, e := range m.panes[0].Entries {
			if e.Name == "notes.txt" {
				m.panes[0].Cursor = i
			}
		}

		m.beginOp(fileops.OpMove)

		if m.pending.Op != fileops.OpUpload {
			t.Fatalf("op = %v", m.pending.Op)
		}
		if m.pending.Host != box || m.pending.Dest != "/srv" {
			t.Fatalf("job = %+v", m.pending)
		}
		if !m.pending.Move {
			t.Fatal("move should set Move")
		}
		if len(m.pending.Srcs) != 1 || filepath.Base(m.pending.Srcs[0]) != "notes.txt" {
			t.Fatalf("srcs = %v", m.pending.Srcs)
		}
	})

	t.Run("delete on the host", func(t *testing.T) {
		m := New()
		remotePane(t, &m, 0, box, "/srv")
		m.active = 0
		cursorTo(t, &m, 0, "app.log")

		m.beginOp(fileops.OpDelete)

		if m.pending.Op != fileops.OpRemoteDelete || m.pending.Host != box {
			t.Fatalf("job = %+v", m.pending)
		}
	})

	t.Run("same host copies without the network", func(t *testing.T) {
		m := New()
		remotePane(t, &m, 0, box, "/srv")
		remotePane(t, &m, 1, box, "/backup")
		m.active = 0
		cursorTo(t, &m, 0, "app.log")

		m.beginOp(fileops.OpCopy)

		if m.pending.Op != fileops.OpRemoteCopy || m.pending.Dest != "/backup" {
			t.Fatalf("job = %+v", m.pending)
		}
	})

	t.Run("two hosts are refused", func(t *testing.T) {
		m := New()
		remotePane(t, &m, 0, box, "/srv")
		remotePane(t, &m, 1, other, "/srv")
		m.active = 0
		cursorTo(t, &m, 0, "app.log")

		m.beginOp(fileops.OpCopy)

		if m.mode == modeConfirm {
			t.Fatal("host-to-host copy should not be offered")
		}
		if !strings.Contains(m.errText, "two hosts") {
			t.Fatalf("errText = %q", m.errText)
		}
	})

	t.Run("archive operations are refused", func(t *testing.T) {
		m := New()
		remotePane(t, &m, 0, box, "/srv")
		m.active = 0
		cursorTo(t, &m, 0, "app.log")

		m.beginOp(fileops.OpPack)

		if m.mode == modeConfirm {
			t.Fatal("pack should not be offered over ssh")
		}
		if !strings.Contains(m.errText, "archive operations") {
			t.Fatalf("errText = %q", m.errText)
		}
	})
}

func TestViewAndEditRefuseRemote(t *testing.T) {
	m := New()
	remotePane(t, &m, 0, remote.Host{Name: "box"}, "/srv")
	m.active = 0
	cursorTo(t, &m, 0, "app.log")

	m.openViewer()
	if !strings.Contains(m.errText, "local-only") {
		t.Fatalf("view errText = %q", m.errText)
	}

	m.errText = ""
	m.openEditor()
	if !strings.Contains(m.errText, "local-only") {
		t.Fatalf("edit errText = %q", m.errText)
	}
}

func TestLocalPrefixLeavesRemote(t *testing.T) {
	dir := t.TempDir()

	m := New()
	remotePane(t, &m, 0, remote.Host{Name: "box"}, "/srv")
	m.active = 0
	m.panes[0].BeginEditPath()
	m.panes[0].SetAddrValue("local:" + dir)

	cmd, err := m.commitAddress()
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if cmd != nil {
		t.Fatal("going local should not schedule a listing")
	}
	if m.panes[0].IsRemote() {
		t.Fatal("pane should be local again")
	}
	if m.panes[0].Path != dir {
		t.Fatalf("path = %q, want %q", m.panes[0].Path, dir)
	}
}
