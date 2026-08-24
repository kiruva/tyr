package app

import "github.com/charmbracelet/bubbles/key"

// keyMap defines every binding tyr responds to.
type keyMap struct {
	Up          key.Binding
	Down        key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Top         key.Binding
	Bottom      key.Binding
	Enter       key.Binding
	Back        key.Binding
	Address     key.Binding
	Switch      key.Binding
	Select      key.Binding
	Hidden      key.Binding
	Sort        key.Binding
	NewFile     key.Binding
	NewDir      key.Binding
	Copy        key.Binding
	Move        key.Binding
	Delete      key.Binding
	Rename      key.Binding
	RenameMulti key.Binding
	Undo        key.Binding
	Pack        key.Binding
	Unpack      key.Binding
	Unwrap      key.Binding
	View        key.Binding
	Edit        key.Binding
	Theme       key.Binding
	Connect     key.Binding
	Help        key.Binding
	Quit        key.Binding
}

func defaultKeys() keyMap {
	return keyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("pgup", "ctrl+u"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("pgdown", "ctrl+d"),
			key.WithHelp("pgdn", "page down"),
		),
		Top: key.NewBinding(
			key.WithKeys("home", "g"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("end", "G"),
			key.WithHelp("G", "bottom"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter", "l", "right"),
			key.WithHelp("enter/l", "open"),
		),
		Back: key.NewBinding(
			key.WithKeys("backspace", "h", "left"),
			key.WithHelp("h", "up dir"),
		),
		Address: key.NewBinding(
			key.WithKeys("ctrl+l", ":"),
			key.WithHelp("ctrl+l/:", "edit path"),
		),
		Switch: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch pane"),
		),
		Select: key.NewBinding(
			key.WithKeys(" "),
			key.WithHelp("space", "select"),
		),
		Hidden: key.NewBinding(
			key.WithKeys("."),
			key.WithHelp(".", "hidden"),
		),
		Sort: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "sort"),
		),
		NewFile: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "new file"),
		),
		NewDir: key.NewBinding(
			key.WithKeys("f7", "N"),
			key.WithHelp("N/F7", "new folder"),
		),
		Copy: key.NewBinding(
			key.WithKeys("f5", "c"),
			key.WithHelp("F5/c", "copy"),
		),
		Move: key.NewBinding(
			key.WithKeys("f6", "m"),
			key.WithHelp("F6/m", "move"),
		),
		Delete: key.NewBinding(
			key.WithKeys("f8", "delete", "d"),
			key.WithHelp("F8/del", "delete"),
		),
		Rename: key.NewBinding(
			key.WithKeys("f2", "r"),
			key.WithHelp("r/F2", "rename"),
		),
		RenameMulti: key.NewBinding(
			key.WithKeys("M"),
			key.WithHelp("M", "multi-rename"),
		),
		Undo: key.NewBinding(
			key.WithKeys("ctrl+z"),
			key.WithHelp("ctrl+z", "undo rename"),
		),
		Pack: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pack"),
		),
		Unpack: key.NewBinding(
			key.WithKeys("u"),
			key.WithHelp("u", "unpack"),
		),
		Unwrap: key.NewBinding(
			key.WithKeys("U"),
			key.WithHelp("U", "unpack here"),
		),
		View: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "view"),
		),
		Edit: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "edit"),
		),
		Theme: key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "theme"),
		),
		Connect: key.NewBinding(
			key.WithKeys("S", "ctrl+s"),
			key.WithHelp("S", "ssh connect"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
	}
}

// helpGroup is a titled cluster of bindings shown in the help overlay.
type helpGroup struct {
	title string
	binds []key.Binding
}

// groups organizes the bindings for the help overlay.
func (k keyMap) groups() []helpGroup {
	return []helpGroup{
		{"Navigate", []key.Binding{k.Up, k.Down, k.PageDown, k.Top, k.Bottom, k.Enter, k.Back, k.Address, k.Switch}},
		{"Select & view", []key.Binding{k.Select, k.Sort, k.Hidden, k.View, k.Edit}},
		{"Create", []key.Binding{k.NewFile, k.NewDir}},
		{"Operations", []key.Binding{k.Copy, k.Move, k.Rename, k.RenameMulti, k.Undo, k.Delete}},
		{"Archives", []key.Binding{k.Pack, k.Unpack, k.Unwrap}},
		{"Remote", []key.Binding{k.Connect}},
		{"App", []key.Binding{k.Theme, k.Help, k.Quit}},
	}
}
