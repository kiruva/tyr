package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/remote"
)

// openModal starts the app with a scratch config and opens the connection modal.
func openModal(t *testing.T, saved ...config.Connection) tea.Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, c := range saved {
		if err := config.SaveConnection(c); err != nil {
			t.Fatal(err)
		}
	}

	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	return m
}

func TestConnPickerListsSaved(t *testing.T) {
	m := openModal(t,
		config.Connection{Name: "prod", Host: "web01", User: "deploy", LastUsed: 200},
		config.Connection{Name: "backup", Host: "nas", LastUsed: 100},
	)

	app := m.(Model)
	if app.mode != modeConn || app.conn.stage != connStageList {
		t.Fatalf("mode = %v stage = %v", app.mode, app.conn.stage)
	}
	view := m.View()
	for _, want := range []string{"prod", "backup", "deploy@web01", "new connection"} {
		if !strings.Contains(view, want) {
			t.Fatalf("picker does not show %q:\n%s", want, view)
		}
	}
}

// TestConnPickerEmptyGoesStraightToForm avoids showing a list with nothing in it.
func TestConnPickerEmptyGoesStraightToForm(t *testing.T) {
	m := openModal(t)

	app := m.(Model)
	if app.conn.stage != connStageForm {
		t.Fatalf("stage = %v, want the form", app.conn.stage)
	}
	if !strings.Contains(m.View(), "New connection") {
		t.Fatal("form is not shown")
	}
}

func TestConnFormSavesWithoutPassword(t *testing.T) {
	m := openModal(t, config.Connection{Name: "existing", Host: "other"})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = typeKeys(t, m, "prod")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeKeys(t, m, "web01.example.com")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeKeys(t, m, "deploy")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // skip port
	m = typeKeys(t, m, "/srv/www")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("saving the form should start a connection attempt")
	}
	app := m.(Model)
	if app.conn.stage != connStageBusy {
		t.Fatalf("stage = %v, want busy", app.conn.stage)
	}

	conns, err := config.Connections()
	if err != nil {
		t.Fatal(err)
	}
	var saved *config.Connection
	for i := range conns {
		if conns[i].Name == "prod" {
			saved = &conns[i]
		}
	}
	if saved == nil {
		t.Fatalf("connection was not saved: %+v", conns)
	}
	if saved.Host != "web01.example.com" || saved.User != "deploy" || saved.Path != "/srv/www" {
		t.Fatalf("saved = %+v", *saved)
	}
}

func TestConnFormRequiresHost(t *testing.T) {
	m := openModal(t)

	m = typeKeys(t, m, "namedonly")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	app := m.(Model)
	if app.conn.stage != connStageForm {
		t.Fatalf("stage = %v, want to stay on the form", app.conn.stage)
	}
	if !strings.Contains(app.conn.status, "host is required") {
		t.Fatalf("status = %q", app.conn.status)
	}
	if conns, _ := config.Connections(); len(conns) != 0 {
		t.Fatalf("an invalid form was saved: %+v", conns)
	}
}

// TestConnFormDefaultsNameToHost keeps the common case to one field.
func TestConnFormDefaultsNameToHost(t *testing.T) {
	m := openModal(t)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // straight to Host
	m = typeKeys(t, m, "example.com")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	conns, _ := config.Connections()
	if len(conns) != 1 || conns[0].Name != "example.com" {
		t.Fatalf("conns = %+v", conns)
	}
}

func TestConnPasswordPromptOnAuthRequired(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01"})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // connect to "prod"
	m, _ = m.Update(connResultMsg{
		target:  config.Connection{Name: "prod", Host: "web01"},
		host:    remote.Host{Name: "web01"},
		paneIdx: 0,
		err:     remote.ErrAuthRequired,
	})

	app := m.(Model)
	if app.conn.stage != connStagePassword {
		t.Fatalf("stage = %v, want the password prompt", app.conn.stage)
	}
	view := m.View()
	if !strings.Contains(view, "Password") || !strings.Contains(view, "not saved") {
		t.Fatalf("password stage does not explain itself:\n%s", view)
	}

	// Typing and submitting hands the password to a fresh attempt, and clears
	// the visible field.
	m = typeKeys(t, m, "hunter2")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("submitting the password should retry the connection")
	}
	app = m.(Model)
	if app.conn.password.Value() != "" {
		t.Fatal("the password input should be cleared once submitted")
	}
	if app.conn.stage != connStageBusy {
		t.Fatalf("stage = %v, want busy", app.conn.stage)
	}
	if !strings.Contains(m.View(), "hunter2") {
		return // expected: the password must not appear anywhere on screen
	}
	t.Fatal("the password is visible in the rendered view")
}

func TestConnWrongPasswordRetries(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m, _ = m.Update(connResultMsg{
		target:   config.Connection{Name: "prod", Host: "web01"},
		host:     remote.Host{Name: "web01"},
		paneIdx:  0,
		password: "wrong",
		err:      errors.New("web01: authentication failed"),
	})

	app := m.(Model)
	if app.conn.stage != connStagePassword {
		t.Fatalf("stage = %v, want another password prompt", app.conn.stage)
	}
	if !strings.Contains(app.conn.status, "authentication failed") {
		t.Fatalf("status = %q", app.conn.status)
	}
}

func TestConnHostKeyConfirmation(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m, _ = m.Update(connResultMsg{
		target:  config.Connection{Name: "prod", Host: "web01"},
		host:    remote.Host{Name: "web01"},
		paneIdx: 0,
		err: &remote.HostKeyError{
			Host:        remote.Host{Name: "web01"},
			KeyType:     "ssh-ed25519",
			Fingerprint: "SHA256:abc123",
		},
	})

	app := m.(Model)
	if app.conn.stage != connStageHostKey {
		t.Fatalf("stage = %v, want the host key prompt", app.conn.stage)
	}
	view := m.View()
	for _, want := range []string{"Unknown host", "SHA256:abc123", "ssh-ed25519", "known_hosts"} {
		if !strings.Contains(view, want) {
			t.Fatalf("host key stage does not show %q:\n%s", want, view)
		}
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if cmd == nil {
		t.Fatal("accepting should retry the connection")
	}
	if m.(Model).conn.stage != connStageBusy {
		t.Fatalf("stage = %v, want busy", m.(Model).conn.stage)
	}
}

// TestConnChangedHostKeyIsNotClickThrough: a known host presenting a new key is
// refused outright rather than offered as a yes/no.
func TestConnChangedHostKeyIsNotClickThrough(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m, _ = m.Update(connResultMsg{
		target:  config.Connection{Name: "prod", Host: "web01"},
		host:    remote.Host{Name: "web01"},
		paneIdx: 0,
		err: &remote.HostKeyError{
			Host: remote.Host{Name: "web01"}, KeyType: "ssh-ed25519",
			Fingerprint: "SHA256:abc123", Changed: true,
		},
	})

	app := m.(Model)
	if app.conn.stage == connStageHostKey {
		t.Fatal("a changed host key must not be offered for confirmation")
	}
	if !strings.Contains(app.conn.status, "CHANGED") {
		t.Fatalf("status = %q", app.conn.status)
	}
}

// TestConnPasswordSurvivesHostKeyStage: confirming a fingerprint after typing a
// password must not ask for the password a second time.
func TestConnPasswordSurvivesHostKeyStage(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(connResultMsg{
		target: config.Connection{Name: "prod", Host: "web01"}, paneIdx: 0,
		err: remote.ErrAuthRequired,
	})
	m = typeKeys(t, m, "hunter2")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	m, _ = m.Update(connResultMsg{
		target: config.Connection{Name: "prod", Host: "web01"}, paneIdx: 0, password: "hunter2",
		err: &remote.HostKeyError{Host: remote.Host{Name: "web01"}, Fingerprint: "SHA256:abc"},
	})
	app := m.(Model)
	if app.conn.stage != connStageHostKey {
		t.Fatalf("stage = %v", app.conn.stage)
	}
	if app.conn.pendingPassword != "hunter2" {
		t.Fatalf("the password was lost across the host key stage: %q", app.conn.pendingPassword)
	}
}

func TestConnSuccessOpensPane(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01", Path: "/srv"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	host := remote.Host{Name: "web01"}
	m, cmd := m.Update(connResultMsg{
		target:  config.Connection{Name: "prod", Host: "web01", Path: "/srv"},
		host:    host,
		paneIdx: 0,
	})

	app := m.(Model)
	if app.mode != modeNormal {
		t.Fatalf("mode = %v, want normal", app.mode)
	}
	if !app.panes[0].IsRemote() || app.panes[0].Host() != host {
		t.Fatalf("pane 0 is not on the host: %+v", app.panes[0].Host())
	}
	if app.panes[0].Path != "/srv" {
		t.Fatalf("path = %q", app.panes[0].Path)
	}
	if app.panes[1].IsRemote() {
		t.Fatal("the other pane must be left alone")
	}
	if cmd == nil {
		t.Fatal("a listing should have been scheduled")
	}
	// Connecting records the use, so the picker offers it first next time.
	conns, _ := config.Connections()
	if conns[0].LastUsed == 0 {
		t.Fatal("LastUsed was not updated")
	}
}

// TestConnOpensInTheOriginatingPane: the modal remembers which pane it came from.
func TestConnOpensInTheOriginatingPane(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := config.SaveConnection(config.Connection{Name: "prod", Host: "web01"}); err != nil {
		t.Fatal(err)
	}

	var m tea.Model = New()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus the right pane
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})

	if got := m.(Model).conn.paneIdx; got != 1 {
		t.Fatalf("paneIdx = %d, want 1", got)
	}
	if !strings.Contains(m.View(), "right pane") {
		t.Fatal("the status bar should say which pane the connection opens in")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(connResultMsg{
		target:  config.Connection{Name: "prod", Host: "web01"},
		host:    remote.Host{Name: "web01"},
		paneIdx: 1,
	})

	app := m.(Model)
	if !app.panes[1].IsRemote() || app.panes[0].IsRemote() {
		t.Fatal("the connection opened in the wrong pane")
	}
}

func TestConnDeleteSaved(t *testing.T) {
	m := openModal(t,
		config.Connection{Name: "keep", Host: "a", LastUsed: 200},
		config.Connection{Name: "drop", Host: "b", LastUsed: 100},
	)

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}}) // onto "drop"
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !m.(Model).conn.deleting {
		t.Fatal("delete should ask first")
	}
	if !strings.Contains(m.View(), "delete drop?") {
		t.Fatalf("no confirmation shown:\n%s", m.View())
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if conns, _ := config.Connections(); len(conns) != 2 {
		t.Fatal("answering n deleted anyway")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	conns, _ := config.Connections()
	if len(conns) != 1 || conns[0].Name != "keep" {
		t.Fatalf("conns = %+v", conns)
	}
}

func TestConnEditRename(t *testing.T) {
	m := openModal(t, config.Connection{Name: "old", Host: "web01", User: "kim"})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	app := m.(Model)
	if app.conn.stage != connStageForm || app.conn.editing != "old" {
		t.Fatalf("stage = %v editing = %q", app.conn.stage, app.conn.editing)
	}
	if got := app.conn.fields[fieldHost].Value(); got != "web01" {
		t.Fatalf("form was not seeded: host = %q", got)
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU}) // clear the name
	m = typeKeys(t, m, "renamed")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	conns, _ := config.Connections()
	if len(conns) != 1 || conns[0].Name != "renamed" || conns[0].User != "kim" {
		t.Fatalf("conns = %+v", conns)
	}
}

func TestConnEscClosesModal(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01"})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	app := m.(Model)
	if app.mode != modeNormal {
		t.Fatalf("mode = %v", app.mode)
	}
	if app.panes[0].IsRemote() {
		t.Fatal("closing the modal should not have connected anything")
	}
}

// TestConnResultIgnoredAfterDismissal: a reply that lands after the modal closed
// must not reopen it or move a pane.
func TestConnResultIgnoredAfterDismissal(t *testing.T) {
	m := openModal(t, config.Connection{Name: "prod", Host: "web01"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})

	m, _ = m.Update(connResultMsg{
		target:  config.Connection{Name: "prod", Host: "web01"},
		host:    remote.Host{Name: "web01"},
		paneIdx: 0,
	})

	app := m.(Model)
	if app.mode != modeNormal {
		t.Fatalf("mode = %v", app.mode)
	}
	if app.panes[0].IsRemote() {
		t.Fatal("a dismissed attempt still opened the pane")
	}
}
