// Package keymap is every action tyr can be asked to do, what each is bound to,
// and the two ways a binding gets changed: by hand in the config file, or by
// pressing the key you want in the editor this package also draws.
//
// The data and its editor live together on purpose. specs is the single list
// that the help overlay, the editor and the config reader all read from, so an
// action added in one place shows up in all three without anything else being
// touched — and there is nowhere for the three to disagree.
package keymap

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// Every binding is described once, in specs below: what it is called in the
// config file, which group it belongs to in the help overlay, what it does, and
// what it is bound to out of the box. Three things read that table — the keymap
// itself, the help overlay, and the key editor — so a binding cannot be
// rebindable in one of them and hard-coded in another.
//
// The help label is generated from whatever the keys currently are rather than
// written by hand, because after a rebinding a hand-written label is a lie.

// Map defines every binding tyr responds to.
type Map struct {
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
	Refresh     key.Binding
	Select      key.Binding
	SelectAll   key.Binding
	Invert      key.Binding
	SelectMask  key.Binding
	Deselect    key.Binding
	Filter      key.Binding
	Find        key.Binding
	SizeDirs    key.Binding
	Props       key.Binding
	Bookmarks   key.Binding
	BookmarkAdd key.Binding
	Shell       key.Binding
	Command     key.Binding
	Sync        key.Binding
	Hidden      key.Binding
	Sort        key.Binding
	NewFile     key.Binding
	NewDir      key.Binding
	Copy        key.Binding
	Move        key.Binding
	Delete      key.Binding
	DeletePerm  key.Binding
	Rename      key.Binding
	RenameMulti key.Binding
	Undo        key.Binding
	Pack        key.Binding
	Unpack      key.Binding
	Unwrap      key.Binding
	View        key.Binding
	Edit        key.Binding
	Theme       key.Binding
	Keys        key.Binding
	Connect     key.Binding
	Help        key.Binding
	Caps        key.Binding
	Quit        key.Binding
}

// Spec describes one bindable action.
type Spec struct {
	id    string   // the name in the config file: key.<id> = …
	group string   // section in the help overlay and the key editor
	desc  string   // what it does, in the help overlay
	def   []string // what it is bound to out of the box
	// field points at this action's binding inside a keymap, so the table can
	// build and rebuild one without reflection.
	field func(*Map) *key.Binding
}

// specs is every action, in help-overlay order.
var specs = []Spec{
	{"up", "Navigate", "up", []string{"up", "k"}, func(k *Map) *key.Binding { return &k.Up }},
	{"down", "Navigate", "down", []string{"down", "j"}, func(k *Map) *key.Binding { return &k.Down }},
	{"pageup", "Navigate", "page up", []string{"pgup", "ctrl+u"}, func(k *Map) *key.Binding { return &k.PageUp }},
	{"pagedown", "Navigate", "page down", []string{"pgdown", "ctrl+d"}, func(k *Map) *key.Binding { return &k.PageDown }},
	{"top", "Navigate", "top", []string{"home", "g"}, func(k *Map) *key.Binding { return &k.Top }},
	{"bottom", "Navigate", "bottom", []string{"end", "G"}, func(k *Map) *key.Binding { return &k.Bottom }},
	{"open", "Navigate", "open", []string{"enter", "l", "right"}, func(k *Map) *key.Binding { return &k.Enter }},
	{"parent", "Navigate", "up dir", []string{"backspace", "h", "left"}, func(k *Map) *key.Binding { return &k.Back }},
	{"address", "Navigate", "edit path", []string{"ctrl+l", ":"}, func(k *Map) *key.Binding { return &k.Address }},
	{"switchpane", "Navigate", "switch pane", []string{"tab"}, func(k *Map) *key.Binding { return &k.Switch }},
	{"refresh", "Navigate", "refresh", []string{"ctrl+r", "R"}, func(k *Map) *key.Binding { return &k.Refresh }},

	{"select", "Select", "select / size dir", []string{" "}, func(k *Map) *key.Binding { return &k.Select }},
	{"selectall", "Select", "select all", []string{"ctrl+a"}, func(k *Map) *key.Binding { return &k.SelectAll }},
	{"invert", "Select", "invert selection", []string{"*"}, func(k *Map) *key.Binding { return &k.Invert }},
	{"selectmask", "Select", "select by mask", []string{"+"}, func(k *Map) *key.Binding { return &k.SelectMask }},
	{"deselectmask", "Select", "deselect by mask", []string{"-"}, func(k *Map) *key.Binding { return &k.Deselect }},

	{"filter", "Look", "filter (esc clears)", []string{"/"}, func(k *Map) *key.Binding { return &k.Filter }},
	{"find", "Look", "find files", []string{"ctrl+f", "F"}, func(k *Map) *key.Binding { return &k.Find }},
	{"sort", "Look", "sort", []string{"s"}, func(k *Map) *key.Binding { return &k.Sort }},
	{"hidden", "Look", "hidden", []string{"."}, func(k *Map) *key.Binding { return &k.Hidden }},
	{"sizedirs", "Look", "size all dirs", []string{"="}, func(k *Map) *key.Binding { return &k.SizeDirs }},
	{"properties", "Look", "properties", []string{"i"}, func(k *Map) *key.Binding { return &k.Props }},
	{"view", "Look", "view", []string{"v"}, func(k *Map) *key.Binding { return &k.View }},
	{"edit", "Look", "edit", []string{"e"}, func(k *Map) *key.Binding { return &k.Edit }},

	{"newfile", "Create", "new file", []string{"n"}, func(k *Map) *key.Binding { return &k.NewFile }},
	{"newdir", "Create", "new folder", []string{"f7", "N"}, func(k *Map) *key.Binding { return &k.NewDir }},

	{"copy", "Operations", "copy", []string{"f5", "c"}, func(k *Map) *key.Binding { return &k.Copy }},
	{"move", "Operations", "move", []string{"f6", "m"}, func(k *Map) *key.Binding { return &k.Move }},
	{"rename", "Operations", "rename", []string{"f2", "r"}, func(k *Map) *key.Binding { return &k.Rename }},
	{"multirename", "Operations", "multi-rename", []string{"M"}, func(k *Map) *key.Binding { return &k.RenameMulti }},
	{"undo", "Operations", "undo", []string{"ctrl+z"}, func(k *Map) *key.Binding { return &k.Undo }},
	{"delete", "Operations", "delete", []string{"f8", "delete", "d"}, func(k *Map) *key.Binding { return &k.Delete }},
	{"deleteperm", "Operations", "delete for good", []string{"shift+f8", "D"}, func(k *Map) *key.Binding { return &k.DeletePerm }},
	{"sync", "Operations", "compare & sync", []string{"f9", "Y"}, func(k *Map) *key.Binding { return &k.Sync }},

	{"pack", "Archives", "pack…", []string{"p"}, func(k *Map) *key.Binding { return &k.Pack }},
	{"unpack", "Archives", "unpack", []string{"u"}, func(k *Map) *key.Binding { return &k.Unpack }},
	{"unpackhere", "Archives", "unpack here", []string{"U"}, func(k *Map) *key.Binding { return &k.Unwrap }},

	{"bookmarks", "Go to", "bookmarks", []string{"b"}, func(k *Map) *key.Binding { return &k.Bookmarks }},
	{"bookmarkadd", "Go to", "bookmark this dir", []string{"B"}, func(k *Map) *key.Binding { return &k.BookmarkAdd }},

	{"shell", "Shell", "drop to a shell", []string{"!"}, func(k *Map) *key.Binding { return &k.Shell }},
	{"command", "Shell", "run a command", []string{"x"}, func(k *Map) *key.Binding { return &k.Command }},

	{"connect", "Remote", "ssh connect", []string{"S", "ctrl+s"}, func(k *Map) *key.Binding { return &k.Connect }},

	{"theme", "App", "theme", []string{"t"}, func(k *Map) *key.Binding { return &k.Theme }},
	{"keys", "App", "edit keys", []string{"K"}, func(k *Map) *key.Binding { return &k.Keys }},
	{"help", "App", "help", []string{"?"}, func(k *Map) *key.Binding { return &k.Help }},
	{"capabilities", "App", "capabilities", []string{"C"}, func(k *Map) *key.Binding { return &k.Caps }},
	{"quit", "App", "quit", []string{"q", "ctrl+c"}, func(k *Map) *key.Binding { return &k.Quit }},
}

// reservedKeys are never given away. Ctrl+C is the way out of anything, and Esc
// is what every prompt in the app answers to; binding either one to something
// else would leave a modal with no exit.
var reservedKeys = map[string]bool{"ctrl+c": true, "esc": true}

// Default is the keymap as it ships.
func Default() Map { return From(nil) }

// From builds a keymap, with any action named in overrides bound to what the
// config says instead of to its default. An override with no keys in it is
// ignored: an action nothing can reach is not something to save someone into.
func From(overrides map[string][]string) Map {
	var k Map
	for _, spec := range specs {
		keys := spec.def
		if custom, ok := overrides[spec.id]; ok && len(custom) > 0 {
			keys = custom
		}
		*spec.field(&k) = key.NewBinding(
			key.WithKeys(keys...),
			key.WithHelp(keyLabel(keys), spec.desc),
		)
	}
	return k
}

// boundKeys is what an action is currently bound to.
func (k Map) boundKeys(spec Spec) []string {
	return spec.field(&k).Keys()
}

// Overrides is every binding that differs from the default, keyed by action id
// — which is all the config file needs to hold.
func (k Map) Overrides() map[string][]string {
	out := map[string][]string{}
	for _, spec := range specs {
		if bound := k.boundKeys(spec); !sameKeys(bound, spec.def) {
			out[spec.id] = bound
		}
	}
	return out
}

// rebind points an action at a new set of keys.
func (k *Map) rebind(spec Spec, keys []string) {
	*spec.field(k) = key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(keys), spec.desc),
	)
}

// boundTo names the action a key already belongs to, so a rebinding cannot
// quietly shadow one that is already there.
func (k Map) boundTo(target string, except string) (Spec, bool) {
	for _, spec := range specs {
		if spec.id == except {
			continue
		}
		for _, bound := range k.boundKeys(spec) {
			if bound == target {
				return spec, true
			}
		}
	}
	return Spec{}, false
}

func sameKeys(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// specByID finds one action's description.
func specByID(id string) (Spec, bool) {
	for _, spec := range specs {
		if spec.id == id {
			return spec, true
		}
	}
	return Spec{}, false
}

// keyLabel is how a set of keys reads in the help overlay: the first two of
// them, prettified, separated by a slash.
func keyLabel(keys []string) string {
	shown := keys
	if len(shown) > 2 {
		shown = shown[:2]
	}
	parts := make([]string, 0, len(shown))
	for _, k := range shown {
		parts = append(parts, prettyKey(k))
	}
	return strings.Join(parts, "/")
}

// keyList is every key an action answers to, for the key editor.
func keyList(keys []string) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, prettyKey(k))
	}
	return strings.Join(parts, " ")
}

// prettyKey turns a Bubble Tea key name into what a person would call it.
func prettyKey(k string) string {
	switch k {
	case " ":
		return "space"
	case "up":
		return "↑"
	case "down":
		return "↓"
	case "left":
		return "←"
	case "right":
		return "→"
	case "backspace":
		return "⌫"
	case "delete":
		return "del"
	case "pgdown":
		return "pgdn"
	}
	// Function keys read better capitalized: f5 is F5, shift+f8 is shift+F8.
	if i := strings.LastIndex(k, "f"); i >= 0 && isFunctionKey(k[i:]) {
		return k[:i] + strings.ToUpper(k[i:])
	}
	return k
}

// isFunctionKey reports whether s is "f" followed by digits.
func isFunctionKey(s string) bool {
	if len(s) < 2 || s[0] != 'f' {
		return false
	}
	for _, r := range s[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// helpGroup is a titled cluster of bindings shown in the help overlay.
type helpGroup struct {
	title string
	binds []key.Binding
}

// groups organizes the bindings for the help overlay, in table order.
func (k Map) groups() []helpGroup {
	var out []helpGroup
	for _, spec := range specs {
		if len(out) == 0 || out[len(out)-1].title != spec.group {
			out = append(out, helpGroup{title: spec.group})
		}
		g := &out[len(out)-1]
		g.binds = append(g.binds, *spec.field(&k))
	}
	return out
}
