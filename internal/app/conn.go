package app

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kiruva/tyr/internal/config"
	"github.com/kiruva/tyr/internal/remote"
)

// The connection modal is a small state machine. Opening it lists the saved
// connections; choosing one tries to connect, and the attempt reports back what
// it still needs — a password, or confirmation of a host key — which moves the
// modal to that stage. Nothing about the credentials is written to disk: the
// saved entry holds where to connect and as whom, never the password.

type connStage int

const (
	connStageList     connStage = iota // pick a saved connection, or add one
	connStageForm                      // enter/edit the details of a connection
	connStagePassword                  // the host wants a password
	connStageHostKey                   // confirm an unrecognised host key
	connStageBusy                      // connecting
)

// connField identifies one input in the new/edit form.
type connField int

const (
	fieldName connField = iota
	fieldHost
	fieldUser
	fieldPort
	fieldPath
	fieldIdentity
	fieldCount
)

var connFieldLabels = [fieldCount]string{
	fieldName:     "Name",
	fieldHost:     "Host",
	fieldUser:     "User",
	fieldPort:     "Port",
	fieldPath:     "Path",
	fieldIdentity: "Key file",
}

var connFieldHints = [fieldCount]string{
	fieldName:     "label for this connection",
	fieldHost:     "hostname or ~/.ssh/config alias",
	fieldUser:     "blank = ssh config or $USER",
	fieldPort:     "blank = 22",
	fieldPath:     "blank = login directory",
	fieldIdentity: "blank = agent, then ~/.ssh/id_*",
}

// connState is everything the modal needs while it is open.
type connState struct {
	stage    connStage
	paneIdx  int // the pane the modal was opened from
	conns    []config.Connection
	cursor   int  // row in the list, len(conns) == the "new connection" row
	deleting bool // the list is asking to confirm a delete

	fields  [fieldCount]textinput.Model
	field   connField
	editing string // name of the connection being edited ("" = adding)
	adHoc   bool   // target came from the address bar and is not saved

	target   config.Connection
	password textinput.Model
	hostKey  *remote.HostKeyError

	// pendingPassword carries an accepted password across the host-key stage, so
	// confirming a fingerprint does not ask for it a second time. It lives only
	// as long as the modal; remote.Connect keeps its own copy for the session.
	pendingPassword string

	status string // error or hint shown inside the modal
}

// connResultMsg reports the outcome of a connection attempt.
type connResultMsg struct {
	target   config.Connection
	host     remote.Host
	paneIdx  int
	password string
	accepted bool // the host key had been confirmed for this attempt
	err      error
}

func newConnInput(placeholder string, password bool, width int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.CharLimit = 0
	ti.Width = width
	if password {
		ti.EchoMode = textinput.EchoPassword
	}
	return ti
}

func newConnState() connState {
	s := connState{password: newConnInput("", true, contentWidth-2)}
	for i := range s.fields {
		s.fields[i] = newConnInput(connFieldHints[i], false, contentWidth-10)
	}
	return s
}

// openConnPicker shows the saved connections for the active pane.
func (m *Model) openConnPicker() tea.Cmd {
	conns, err := config.Connections()
	m.conn = newConnState()
	m.conn.paneIdx = m.active
	m.conn.conns = conns
	m.conn.stage = connStageList
	if err != nil {
		m.conn.status = "could not read saved connections: " + err.Error()
	}
	if len(conns) == 0 {
		// Nothing saved yet — go straight to the form rather than showing an
		// empty list with one "add" row.
		return m.connBeginForm(config.Connection{})
	}
	m.mode = modeConn
	return nil
}

// openConnForAddress starts the flow for a target typed into the address bar.
// It is treated as an unsaved connection, with the option to save it once it
// works.
func (m *Model) openConnForAddress(h remote.Host, path string) tea.Cmd {
	m.conn = newConnState()
	m.conn.paneIdx = m.active
	m.conn.adHoc = true
	m.conn.target = config.Connection{
		Name: h.Name,
		Host: h.Name,
		User: h.User,
		Port: h.Port,
		Path: path,
	}
	m.mode = modeConn
	return m.connAttempt("", false)
}

// connBeginForm opens the form, seeded from c (zero c = a new connection).
func (m *Model) connBeginForm(c config.Connection) tea.Cmd {
	m.conn.stage = connStageForm
	m.conn.editing = c.Name
	m.conn.status = ""
	values := [fieldCount]string{
		fieldName:     c.Name,
		fieldHost:     c.Host,
		fieldUser:     c.User,
		fieldPort:     c.Port,
		fieldPath:     c.Path,
		fieldIdentity: c.Identity,
	}
	for i := range m.conn.fields {
		m.conn.fields[i].SetValue(values[i])
		m.conn.fields[i].Blur()
	}
	m.conn.field = fieldName
	m.mode = modeConn
	return m.conn.fields[fieldName].Focus()
}

// connFormValues assembles a Connection from the form.
func (s *connState) connFormValues() config.Connection {
	value := func(f connField) string { return strings.TrimSpace(s.fields[f].Value()) }
	return config.Connection{
		Name:     value(fieldName),
		Host:     value(fieldHost),
		User:     value(fieldUser),
		Port:     value(fieldPort),
		Path:     value(fieldPath),
		Identity: value(fieldIdentity),
	}
}

// connAttempt tries to connect to the current target off the UI thread.
func (m *Model) connAttempt(password string, acceptHostKey bool) tea.Cmd {
	target := m.conn.target
	paneIdx := m.conn.paneIdx
	host := connHost(target)

	m.conn.stage = connStageBusy
	m.conn.status = ""

	return func() tea.Msg {
		err := remote.Connect(host, remote.Options{
			Password:      password,
			IdentityFile:  target.Identity,
			AcceptHostKey: acceptHostKey,
		})
		return connResultMsg{
			target:   target,
			host:     host,
			paneIdx:  paneIdx,
			password: password,
			accepted: acceptHostKey,
			err:      err,
		}
	}
}

// connHost maps a saved connection onto the ssh destination.
func connHost(c config.Connection) remote.Host {
	return remote.Host{User: c.User, Name: c.Host, Port: c.Port}
}

// onConnResult routes a finished attempt: connected, needs a password, needs a
// host key decision, or failed outright.
func (m Model) onConnResult(msg connResultMsg) (tea.Model, tea.Cmd) {
	if m.mode != modeConn || m.conn.paneIdx != msg.paneIdx {
		return m, nil // the modal was dismissed while we were dialling
	}

	switch {
	case msg.err == nil:
		return m.connSucceeded(msg)

	case errors.Is(msg.err, remote.ErrAuthRequired):
		m.conn.stage = connStagePassword
		m.conn.password.SetValue("")
		m.conn.status = "key and agent authentication did not get in"
		cmd := m.conn.password.Focus()
		return m, cmd
	}

	var hk *remote.HostKeyError
	if errors.As(msg.err, &hk) {
		if hk.Changed {
			// A known host presenting a different key is not something to click
			// through: it is either a rebuilt server or an interception.
			m.conn.stage = connStageList
			m.conn.status = hk.Error() + " — verify the host, then fix ~/.ssh/known_hosts by hand"
			return m, nil
		}
		m.conn.stage = connStageHostKey
		m.conn.hostKey = hk
		m.conn.status = ""
		return m, nil
	}

	// A wrong password lands here; keep the prompt up so it can be retried.
	if msg.password != "" {
		m.conn.stage = connStagePassword
		m.conn.password.SetValue("")
		m.conn.status = msg.err.Error()
		cmd := m.conn.password.Focus()
		return m, cmd
	}
	m.conn.stage = connStageList
	m.conn.status = msg.err.Error()
	return m, nil
}

// connSucceeded opens the pane on the host and records the connection's use.
func (m Model) connSucceeded(msg connResultMsg) (tea.Model, tea.Cmd) {
	if !m.conn.adHoc && msg.target.Name != "" {
		if err := config.TouchConnection(msg.target.Name); err != nil {
			m.errText = "connected, but could not update the config: " + err.Error()
		}
	}

	m.mode = modeNormal
	m.conn = connState{}
	m.errText = ""
	cmd := m.openRemote(msg.paneIdx, msg.host, msg.target.Path)
	return m, cmd
}

// onConnKey dispatches a keypress to the modal's current stage.
func (m Model) onConnKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		remote.ForgetAll()
		return m, tea.Quit
	}

	switch m.conn.stage {
	case connStageList:
		return m.onConnListKey(msg)
	case connStageForm:
		return m.onConnFormKey(msg)
	case connStagePassword:
		return m.onConnPasswordKey(msg)
	case connStageHostKey:
		return m.onConnHostKeyKey(msg)
	default: // connStageBusy — the attempt is in flight
		if msg.String() == "esc" {
			m.mode = modeNormal
			m.conn = connState{}
		}
		return m, nil
	}
}

func (m Model) onConnListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := len(m.conn.conns) + 1 // the last row adds a connection

	if m.conn.deleting {
		switch msg.String() {
		case "y":
			name := m.conn.conns[m.conn.cursor].Name
			m.conn.deleting = false
			if err := config.DeleteConnection(name); err != nil {
				m.conn.status = "could not delete: " + err.Error()
				return m, nil
			}
			conns, err := config.Connections()
			if err != nil {
				m.conn.status = err.Error()
			}
			m.conn.conns = conns
			if m.conn.cursor > len(conns) {
				m.conn.cursor = len(conns)
			}
			m.conn.status = "deleted " + name
		default:
			m.conn.deleting = false
		}
		return m, nil
	}

	switch msg.String() {
	case "esc", "q":
		m.mode = modeNormal
		m.conn = connState{}
	case "up", "k":
		m.conn.cursor = max(m.conn.cursor-1, 0)
	case "down", "j":
		m.conn.cursor = min(m.conn.cursor+1, rows-1)
	case "n":
		cmd := m.connBeginForm(config.Connection{})
		return m, cmd
	case "e":
		if m.conn.cursor < len(m.conn.conns) {
			cmd := m.connBeginForm(m.conn.conns[m.conn.cursor])
			return m, cmd
		}
	case "d", "delete":
		if m.conn.cursor < len(m.conn.conns) {
			m.conn.deleting = true
		}
	case "enter":
		if m.conn.cursor >= len(m.conn.conns) {
			cmd := m.connBeginForm(config.Connection{})
			return m, cmd
		}
		m.conn.target = m.conn.conns[m.conn.cursor]
		m.conn.adHoc = false
		cmd := m.connAttempt("", false)
		return m, cmd
	}
	return m, nil
}

func (m Model) onConnFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if len(m.conn.conns) == 0 {
			m.mode = modeNormal
			m.conn = connState{}
			return m, nil
		}
		m.conn.stage = connStageList
		m.conn.status = ""
		return m, nil

	case "tab", "down":
		cmd := m.connFocusField(m.conn.field + 1)
		return m, cmd
	case "shift+tab", "up":
		cmd := m.connFocusField(m.conn.field - 1)
		return m, cmd

	case "enter":
		return m.connSaveForm()
	}

	var cmd tea.Cmd
	m.conn.fields[m.conn.field], cmd = m.conn.fields[m.conn.field].Update(msg)
	return m, cmd
}

// connFocusField moves focus, wrapping at both ends.
func (m *Model) connFocusField(next connField) tea.Cmd {
	if next < 0 {
		next = fieldCount - 1
	}
	if next >= fieldCount {
		next = 0
	}
	m.conn.fields[m.conn.field].Blur()
	m.conn.field = next
	return m.conn.fields[next].Focus()
}

// connSaveForm validates the form, persists the connection, and connects.
func (m Model) connSaveForm() (tea.Model, tea.Cmd) {
	c := m.conn.connFormValues()
	if c.Host == "" {
		m.conn.status = "a host is required"
		cmd := m.connFocusField(fieldHost)
		return m, cmd
	}
	if c.Name == "" {
		c.Name = c.Host // a sensible default label
	}
	if err := config.ValidConnectionName(c.Name); err != nil {
		m.conn.status = err.Error()
		cmd := m.connFocusField(fieldName)
		return m, cmd
	}
	if m.conn.editing != "" && !strings.EqualFold(m.conn.editing, c.Name) {
		// Renaming: drop the old key so the list does not show both.
		if err := config.DeleteConnection(m.conn.editing); err != nil {
			m.conn.status = "could not rename: " + err.Error()
			return m, nil
		}
	}

	c.LastUsed = time.Now().Unix()
	if err := config.SaveConnection(c); err != nil {
		m.conn.status = "could not save: " + err.Error()
		return m, nil
	}

	m.conn.fields[m.conn.field].Blur()
	m.conn.target = c
	m.conn.adHoc = false
	m.conn.editing = ""
	cmd := m.connAttempt("", false)
	return m, cmd
}

func (m Model) onConnPasswordKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.conn.password.SetValue("")
		if m.conn.adHoc || len(m.conn.conns) == 0 {
			m.mode = modeNormal
			m.conn = connState{}
			return m, nil
		}
		m.conn.stage = connStageList
		m.conn.status = ""
		return m, nil

	case "enter":
		pw := m.conn.password.Value()
		if pw == "" {
			m.conn.status = "enter a password, or esc to go back"
			return m, nil
		}
		m.conn.password.SetValue("")
		m.conn.password.Blur()
		m.conn.pendingPassword = pw
		// The host key may still need confirming; that stage re-attempts with
		// the same password, which is why the modal holds on to it.
		cmd := m.connAttempt(pw, m.conn.hostKey != nil)
		return m, cmd
	}

	var cmd tea.Cmd
	m.conn.password, cmd = m.conn.password.Update(msg)
	return m, cmd
}

func (m Model) onConnHostKeyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "a", "y", "enter":
		cmd := m.connAttempt(m.conn.lastPassword(), true)
		return m, cmd
	default:
		m.conn.hostKey = nil
		if m.conn.adHoc || len(m.conn.conns) == 0 {
			m.mode = modeNormal
			m.conn = connState{}
			return m, nil
		}
		m.conn.stage = connStageList
		m.conn.status = "cancelled — host key was not accepted"
		return m, nil
	}
}

// lastPassword is empty unless the flow already collected one; the host key
// stage must not lose it, or accepting the key would demand it again.
func (s *connState) lastPassword() string { return s.pendingPassword }

// connSummary renders the target for the password and host-key stages.
func (s *connState) connSummary() string {
	h := connHost(s.target)
	if s.target.Name != "" && !strings.EqualFold(s.target.Name, s.target.Host) {
		return fmt.Sprintf("%s (%s)", s.target.Name, h.String())
	}
	return h.String()
}

// updateConnInputs forwards non-key messages (cursor blink) to whichever input
// the modal currently shows. The receiver is a pointer because the input models
// carry their own state; a copy would drop the blink.
func (m *Model) updateConnInputs(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.conn.stage {
	case connStageForm:
		m.conn.fields[m.conn.field], cmd = m.conn.fields[m.conn.field].Update(msg)
	case connStagePassword:
		m.conn.password, cmd = m.conn.password.Update(msg)
	}
	return cmd
}
