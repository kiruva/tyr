package keymap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"

	"github.com/kiruva/tyr/internal/config"
)

// isolate points the config at a directory this test owns, so a rebind writes
// somewhere disposable rather than over the real keymap.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func press(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// at puts the cursor on a named action and returns the editor there.
func at(t *testing.T, e Editor, id string) Editor {
	t.Helper()
	index, ok := ActionIndex(id)
	if !ok {
		t.Fatalf("no such action: %s", id)
	}
	return e.MoveTo(index)
}

// bound is what an action answers to in this map.
func bound(t *testing.T, k Map, id string) []string {
	t.Helper()
	spec, ok := specByID(id)
	if !ok {
		t.Fatalf("no such action: %s", id)
	}
	return k.boundKeys(spec)
}

// Navigation ------------------------------------------------------------------

func TestEditorNavigationClampsAtBothEnds(t *testing.T) {
	isolate(t)
	last := Actions() - 1

	e, keys := NewEditor(), Default()
	for range Actions() + 5 {
		e, keys, _ = e.Update(press("j"), keys)
	}
	if e.Cursor() != last {
		t.Errorf("cursor = %d, want %d", e.Cursor(), last)
	}
	for range Actions() + 5 {
		e, keys, _ = e.Update(press("k"), keys)
	}
	if e.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0", e.Cursor())
	}
}

func TestEditorArrowKeysMoveToo(t *testing.T) {
	isolate(t)
	e, keys := NewEditor(), Default()

	e, keys, _ = e.Update(press("down"), keys)
	if e.Cursor() != 1 {
		t.Fatalf("down: cursor = %d, want 1", e.Cursor())
	}
	e, _, _ = e.Update(press("up"), keys)
	if e.Cursor() != 0 {
		t.Errorf("up: cursor = %d, want 0", e.Cursor())
	}
}

func TestEditorPagingMovesByAPageAndClamps(t *testing.T) {
	isolate(t)
	e, keys := NewEditor(), Default()

	e, keys, _ = e.Update(press("pgdown"), keys)
	if e.Cursor() != pageRows {
		t.Fatalf("pgdown: cursor = %d, want %d", e.Cursor(), pageRows)
	}
	e, keys, _ = e.Update(press("pgup"), keys)
	if e.Cursor() != 0 {
		t.Fatalf("pgup: cursor = %d, want 0", e.Cursor())
	}
	// Paging off the near end stops at the end rather than going negative.
	e, _, _ = e.Update(press("pgup"), keys)
	if e.Cursor() != 0 {
		t.Errorf("pgup at the top: cursor = %d, want 0", e.Cursor())
	}
}

func TestEditorJumpsToEitherEnd(t *testing.T) {
	isolate(t)
	e, keys := NewEditor(), Default()

	for _, k := range []string{"end", "G"} {
		e, keys, _ = e.Update(press(k), keys)
		if e.Cursor() != Actions()-1 {
			t.Errorf("%q: cursor = %d, want the last row", k, e.Cursor())
		}
	}
	for _, k := range []string{"home", "g"} {
		e, keys, _ = e.Update(press(k), keys)
		if e.Cursor() != 0 {
			t.Errorf("%q: cursor = %d, want 0", k, e.Cursor())
		}
	}
}

func TestMoveToClampsOutOfRangeRows(t *testing.T) {
	if got := NewEditor().MoveTo(-5).Cursor(); got != 0 {
		t.Errorf("MoveTo(-5) = %d, want 0", got)
	}
	if got, want := NewEditor().MoveTo(9999).Cursor(), Actions()-1; got != want {
		t.Errorf("MoveTo(9999) = %d, want %d", got, want)
	}
}

func TestActionIndexFindsAnActionAndRejectsAnUnknownOne(t *testing.T) {
	if _, ok := ActionIndex("copy"); !ok {
		t.Error(`ActionIndex("copy") not found`)
	}
	if _, ok := ActionIndex("no-such-action"); ok {
		t.Error("ActionIndex found an action that does not exist")
	}
}

// Closing ---------------------------------------------------------------------

func TestEditorClosesAndForgetsItsState(t *testing.T) {
	isolate(t)
	for _, k := range []string{"esc", "q"} {
		e, keys := at(t, NewEditor(), "copy"), Default()
		e, _, res := e.Update(press(k), keys)

		if res.Outcome != Close {
			t.Errorf("%q: Outcome = %v, want Close", k, res.Outcome)
		}
		if e.Cursor() != 0 || e.Capturing() {
			t.Errorf("%q: the editor was not reset (cursor %d, capturing %v)", k, e.Cursor(), e.Capturing())
		}
	}
}

func TestEditorCtrlCQuitsRatherThanClosing(t *testing.T) {
	isolate(t)
	_, _, res := NewEditor().Update(press("ctrl+c"), Default())

	if res.Outcome != Stay {
		t.Errorf("Outcome = %v, want Stay", res.Outcome)
	}
	if res.Cmd == nil {
		t.Fatal("Cmd = nil, want tea.Quit")
	}
	if _, isQuit := res.Cmd().(tea.QuitMsg); !isQuit {
		t.Error("Cmd is not tea.Quit")
	}
}

// Capturing a key -------------------------------------------------------------

func TestEnterStartsCapturingAndEscapeBacksOut(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()
	before := bound(t, keys, "copy")

	e, keys, _ = e.Update(press("enter"), keys)
	if !e.Capturing() {
		t.Fatal("enter did not start capturing")
	}

	e, keys, res := e.Update(press("esc"), keys)
	if e.Capturing() {
		t.Error("esc did not leave capture")
	}
	if res.Outcome != Stay {
		t.Errorf("Outcome = %v, want Stay: esc in capture backs out, it does not close", res.Outcome)
	}
	if got := bound(t, keys, "copy"); !sameKeys(got, before) {
		t.Errorf("backing out changed the binding to %v", got)
	}
}

func TestCapturedKeyReplacesTheBindingAndIsSaved(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()

	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("z"), keys)

	if got := bound(t, keys, "copy"); !sameKeys(got, []string{"z"}) {
		t.Fatalf("copy is bound to %v, want just z", got)
	}
	if e.Capturing() {
		t.Error("the editor stayed in capture after binding")
	}
	if status, refused := e.Status(); refused || !strings.Contains(status, "z") {
		t.Errorf("status = %q (refused %v), want it to report the new key", status, refused)
	}

	// The change is on disk, not only in memory: there is no save step.
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Keys["copy"]; !sameKeys(got, []string{"z"}) {
		t.Errorf("the config says copy is %v, want z", got)
	}
}

func TestAddAppendsAKeyRatherThanReplacing(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()
	before := bound(t, keys, "copy")

	e, keys, _ = e.Update(press("a"), keys)
	if !e.Capturing() {
		t.Fatal("'a' did not start capturing")
	}
	_, keys, _ = e.Update(press("z"), keys)

	got := bound(t, keys, "copy")
	if len(got) != len(before)+1 {
		t.Fatalf("copy is bound to %v, want %v plus z", got, before)
	}
	if got[len(got)-1] != "z" {
		t.Errorf("copy is bound to %v, want z appended last", got)
	}
	for i, k := range before {
		if got[i] != k {
			t.Fatalf("copy is bound to %v, want the original %v kept in front", got, before)
		}
	}
}

// A key already spoken for is refused rather than stolen: two actions on one key
// is not a preference.
func TestCapturingARefusesAKeyAlreadyBound(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()
	before := bound(t, keys, "copy")

	// Whatever "view" answers to, copy may not take it.
	viewKey := bound(t, keys, "view")[0]

	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press(viewKey), keys)

	status, refused := e.Status()
	if !refused {
		t.Fatalf("taking %q was allowed; status = %q", viewKey, status)
	}
	if !strings.Contains(status, "view") {
		t.Errorf("status = %q, want it to name the action that has the key", status)
	}
	if got := bound(t, keys, "copy"); !sameKeys(got, before) {
		t.Errorf("copy changed to %v despite the refusal", got)
	}
	if e.Capturing() {
		t.Error("a refusal should return to the list")
	}
}

// Ctrl+C is how you get out of things, so it cannot be given away. Esc is
// reserved for the same reason but never reaches this check: in capture it
// means "back out", which TestEnterStartsCapturingAndEscapeBacksOut covers.
func TestCapturingRefusesReservedKeys(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()
	before := bound(t, keys, "copy")

	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("ctrl+c"), keys)

	status, refused := e.Status()
	if !refused {
		t.Fatalf("ctrl+c was accepted as a binding; status = %q", status)
	}
	if !strings.Contains(status, "reserved") {
		t.Errorf("status = %q, want it to say the key is reserved", status)
	}
	if got := bound(t, keys, "copy"); !sameKeys(got, before) {
		t.Errorf("copy changed to %v despite the refusal", got)
	}
	if e.Capturing() {
		t.Error("a refusal should return to the list")
	}
}

// Resetting -------------------------------------------------------------------

func TestResetOneRestoresTheDefault(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()
	def := bound(t, keys, "copy")

	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("z"), keys)
	if sameKeys(bound(t, keys, "copy"), def) {
		t.Fatal("setup failed: copy was not rebound")
	}

	e, keys, _ = e.Update(press("d"), keys)
	if got := bound(t, keys, "copy"); !sameKeys(got, def) {
		t.Errorf("copy is %v, want the default %v back", got, def)
	}
	if _, refused := e.Status(); refused {
		t.Error("resetting was refused")
	}
	// Back at its default, the action is no longer an override.
	if _, ok := keys.Overrides()["copy"]; ok {
		t.Error("copy is still listed as an override after being reset")
	}
}

func TestResetOneSaysSoWhenAlreadyDefault(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()

	e, keys, _ = e.Update(press("d"), keys)
	status, refused := e.Status()
	if refused {
		t.Error("already being default is a note, not a refusal")
	}
	if !strings.Contains(status, "already the default") {
		t.Errorf("status = %q, want it to say the action is already default", status)
	}
	if n := len(keys.Overrides()); n != 0 {
		t.Errorf("resetting an unchanged action created %d overrides", n)
	}
}

// A default can collide with something the user has since bound elsewhere, and
// resetting into that collision has to be refused rather than silently shadow it.
func TestResetOneRefusesWhenTheDefaultIsNowTakenElsewhere(t *testing.T) {
	isolate(t)
	keys := Default()
	copyDefault := bound(t, keys, "copy")[0]

	// Move copy off its default key, then give that key to view.
	e := at(t, NewEditor(), "copy")
	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("z"), keys)

	e = at(t, e, "view")
	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press(copyDefault), keys)
	if status, refused := e.Status(); refused {
		t.Fatalf("setup failed: view could not take %q: %s", copyDefault, status)
	}

	// Now copy cannot go home again.
	e = at(t, e, "copy")
	e, keys, _ = e.Update(press("d"), keys)

	status, refused := e.Status()
	if !refused {
		t.Fatalf("the reset was allowed; status = %q", status)
	}
	if !strings.Contains(status, "cannot reset") {
		t.Errorf("status = %q, want it to say the reset cannot happen", status)
	}
	if got := bound(t, keys, "copy"); !sameKeys(got, []string{"z"}) {
		t.Errorf("copy is %v, want it left on z", got)
	}
}

func TestResetAllClearsEveryOverride(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()

	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("z"), keys)
	e = at(t, e, "view")
	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("y"), keys)
	if len(keys.Overrides()) != 2 {
		t.Fatalf("setup failed: %d overrides, want 2", len(keys.Overrides()))
	}

	e, keys, _ = e.Update(press("D"), keys)

	if n := len(keys.Overrides()); n != 0 {
		t.Errorf("%d overrides survived a reset-all", n)
	}
	if status, refused := e.Status(); refused || !strings.Contains(status, "default") {
		t.Errorf("status = %q (refused %v), want it to report the reset", status, refused)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Keys) != 0 {
		t.Errorf("the config still holds %v", cfg.Keys)
	}
}

// A binding that took effect but could not be written down says so, rather than
// looking like it worked and coming back changed on the next run.
func TestBindingReportsAConfigThatCannotBeWritten(t *testing.T) {
	// A regular file where the config directory should be: MkdirAll fails.
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocker)

	e, keys := at(t, NewEditor(), "copy"), Default()
	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("z"), keys)

	status, refused := e.Status()
	if !refused {
		t.Fatalf("an unwritable config was not reported; status = %q", status)
	}
	if !strings.Contains(status, "not saved") {
		t.Errorf("status = %q, want it to say the binding was not saved", status)
	}
	// The binding is still live for this run: the failure is the writing, not
	// the rebinding.
	if got := bound(t, keys, "copy"); !sameKeys(got, []string{"z"}) {
		t.Errorf("copy is %v, want z to be live regardless", got)
	}
}

func TestResetAllReportsAConfigThatCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocker)

	e, _, _ := NewEditor().Update(press("D"), Default())
	status, refused := e.Status()
	if !refused {
		t.Fatalf("an unwritable config was not reported; status = %q", status)
	}
	if !strings.Contains(status, "config") {
		t.Errorf("status = %q, want it to name the config", status)
	}
}

// An unrecognised key in the list does nothing at all, rather than falling
// through to something.
func TestUnhandledKeyIsIgnored(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()

	next, after, res := e.Update(press("Z"), keys)
	if res.Outcome != Stay || res.Cmd != nil {
		t.Errorf("Outcome = %v, Cmd = %v; want Stay and no command", res.Outcome, res.Cmd)
	}
	if next.Cursor() != e.Cursor() || next.Capturing() {
		t.Error("an unhandled key moved the editor")
	}
	if len(after.Overrides()) != 0 {
		t.Error("an unhandled key changed a binding")
	}
}

// Config marshalling ---------------------------------------------------------

// The config file is a line of text, so the two key names that would make it
// ambiguous are written as words and must survive the round trip.
func TestSpaceAndCommaRoundTripThroughTheConfig(t *testing.T) {
	for raw, named := range map[string]string{" ": "space", ",": "comma", "j": "j"} {
		if got := toConfigKey(raw); got != named {
			t.Errorf("toConfigKey(%q) = %q, want %q", raw, got, named)
		}
		if got := fromConfigKey(named); got != raw {
			t.Errorf("fromConfigKey(%q) = %q, want %q", named, got, raw)
		}
	}
	// Whitespace and case in a hand-edited file are forgiven.
	if got := fromConfigKey("  SPACE  "); got != " " {
		t.Errorf("fromConfigKey(%q) = %q, want a space", "  SPACE  ", got)
	}
}

func TestToConfigKeysRendersEveryBinding(t *testing.T) {
	got := toConfigKeys(map[string][]string{"select": {" ", ","}})
	want := []string{"space", "comma"}
	if !sameKeys(got["select"], want) {
		t.Errorf("toConfigKeys = %v, want %v", got["select"], want)
	}
}

// A misspelled action in the file would otherwise do nothing, quietly; it comes
// back named so the app can say so.
func TestFromConfigDropsAndNamesUnknownActions(t *testing.T) {
	binds, unknown := FromConfig(map[string][]string{
		"copy":      {"space"},
		"nonsense":  {"x"},
		"alsobogus": {"y"},
	})

	if _, ok := binds["copy"]; !ok {
		t.Error("a known action was dropped")
	}
	if got := binds["copy"]; !sameKeys(got, []string{" "}) {
		t.Errorf("copy = %v, want the space it names", got)
	}
	if _, ok := binds["nonsense"]; ok {
		t.Error("an unknown action was kept")
	}
	if len(unknown) != 2 {
		t.Errorf("unknown = %v, want both bogus actions named", unknown)
	}
}

// Map ------------------------------------------------------------------------

func TestBoundToSkipsTheActionAsking(t *testing.T) {
	keys := Default()
	spec, _ := specByID("copy")
	own := keys.boundKeys(spec)[0]

	// An action does not collide with itself, or rebinding would be impossible.
	if other, taken := keys.boundTo(own, "copy"); taken {
		t.Errorf("copy collides with itself via %s", other.id)
	}
	if _, taken := keys.boundTo(own, "view"); !taken {
		t.Errorf("%q should be reported as taken when someone else asks", own)
	}
	if _, taken := keys.boundTo("ctrl+alt+shift+f24", ""); taken {
		t.Error("an unbound key was reported as taken")
	}
}

func TestSameKeys(t *testing.T) {
	for _, tt := range []struct {
		a, b []string
		want bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a"}, []string{"a", "b"}, false},
		{[]string{"a", "b"}, []string{"b", "a"}, false}, // order is part of it
		{nil, nil, true},
	} {
		if got := sameKeys(tt.a, tt.b); got != tt.want {
			t.Errorf("sameKeys(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestKeyListNamesEveryKey(t *testing.T) {
	if got := keyList([]string{"f8", "delete", " "}); got != "F8 del space" {
		t.Errorf("keyList = %q, want all three prettified", got)
	}
	if got := keyList(nil); got != "" {
		t.Errorf("keyList(nil) = %q, want empty", got)
	}
}

// An override with no keys is ignored: an action nothing can reach is not
// something to save someone into.
func TestFromIgnoresAnEmptyOverride(t *testing.T) {
	def := Default()
	keys := From(map[string][]string{"copy": {}})

	spec, _ := specByID("copy")
	if got, want := keys.boundKeys(spec), def.boundKeys(spec); !sameKeys(got, want) {
		t.Errorf("copy = %v, want the default %v", got, want)
	}
}

func TestGroupsCoverEveryActionInTableOrder(t *testing.T) {
	groups := Default().groups()
	if len(groups) == 0 {
		t.Fatal("no groups")
	}

	total := 0
	seen := map[string]bool{}
	for _, g := range groups {
		if g.title == "" {
			t.Error("a group has no title")
		}
		if seen[g.title] {
			t.Errorf("group %q appears twice: the table is out of order", g.title)
		}
		seen[g.title] = true
		total += len(g.binds)
	}
	if total != Actions() {
		t.Errorf("the groups hold %d bindings, want all %d actions", total, Actions())
	}
}

// View -----------------------------------------------------------------------

func TestEditorViewListsActionsAndTheirKeys(t *testing.T) {
	isolate(t)
	view := NewEditor().View(Default(), 100, 30)

	for _, want := range []string{"keys", "action", "enter rebind", "esc close"} {
		if !strings.Contains(view, want) {
			t.Errorf("the editor does not mention %q:\n%s", want, view)
		}
	}
}

func TestEditorViewPromptsWhileCapturing(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()

	e, keys, _ = e.Update(press("enter"), keys)
	if view := e.View(keys, 100, 30); !strings.Contains(view, "press a key") {
		t.Errorf("a capturing editor does not prompt:\n%s", view)
	}

	e = at(t, NewEditor(), "copy")
	e, keys, _ = e.Update(press("a"), keys)
	if view := e.View(keys, 100, 30); !strings.Contains(view, "to add") {
		t.Errorf("an adding editor does not say so:\n%s", view)
	}
}

func TestEditorViewCountsChangesInTheHeader(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()

	if view := e.View(keys, 100, 30); strings.Contains(view, "change") {
		t.Errorf("an unchanged keymap should not report changes:\n%s", view)
	}

	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("z"), keys)
	if view := e.View(keys, 100, 30); !strings.Contains(view, "1 change") {
		t.Errorf("the header does not report the change:\n%s", view)
	}
}

func TestEditorViewShowsAStatusAndARefusal(t *testing.T) {
	isolate(t)
	e, keys := at(t, NewEditor(), "copy"), Default()
	e, keys, _ = e.Update(press("d"), keys) // "already the default"

	if view := e.View(keys, 100, 30); !strings.Contains(view, "already the default") {
		t.Errorf("the footer does not carry the status:\n%s", view)
	}
}

// The editor is full-screen, so it has to stay inside the window at any size
// and survive a terminal too short for its own chrome.
func TestEditorViewFitsAnySize(t *testing.T) {
	isolate(t)
	e, keys := NewEditor(), Default()

	for _, size := range []struct{ w, h int }{
		{40, 10}, {60, 24}, {100, 30}, {200, 60},
		{20, 5}, // narrower and shorter than the chrome wants
		{10, 1}, // degenerate: must not panic or go negative
	} {
		view := e.View(keys, size.w, size.h)
		for i, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > size.w {
				t.Errorf("%dx%d: line %d is %d columns wide", size.w, size.h, i, w)
			}
		}
	}
}

func TestItemCountIsSingularForOne(t *testing.T) {
	if got := itemCount(1, "change"); got != "1 change" {
		t.Errorf("itemCount(1) = %q", got)
	}
	if got := itemCount(3, "change"); got != "3 changes" {
		t.Errorf("itemCount(3) = %q", got)
	}
	if got := itemCount(0, "change"); got != "0 changes" {
		t.Errorf("itemCount(0) = %q", got)
	}
}

// Help overlay ---------------------------------------------------------------

func TestHelpListsTheGroupsAndFollowsARebinding(t *testing.T) {
	isolate(t)

	// Tall enough that nothing is scrolled out: a clipped overlay legitimately
	// omits the groups below the window, which is what TestHelpScrolls covers.
	help := Help(Default(), 100, 200, 0)
	if !strings.Contains(help, "tyr — keys") {
		t.Errorf("the help overlay has no title:\n%s", help)
	}
	for _, g := range Default().groups() {
		if !strings.Contains(help, g.title) {
			t.Errorf("the help overlay omits the %q group", g.title)
		}
	}

	// The overlay is generated from the map, so a rebinding shows up in it.
	e, keys := at(t, NewEditor(), "copy"), Default()
	_, keys, _ = e.Update(press("enter"), keys)
	_, keys, _ = e.Update(press("z"), keys)
	if !strings.Contains(Help(keys, 100, 200, 0), "z") {
		t.Error("the help overlay does not show a rebound key")
	}
}

func TestHelpScrollsWhenItDoesNotFit(t *testing.T) {
	isolate(t)
	tall := Help(Default(), 100, 200, 0) // everything fits
	short := Help(Default(), 100, 6, 0)  // it does not

	if !strings.Contains(tall, "any key to close") {
		t.Errorf("the full overlay has no footer:\n%s", tall)
	}
	if !strings.Contains(short, "↑/↓ more") {
		t.Errorf("a clipped overlay does not offer scrolling:\n%s", short)
	}
	// Scrolling changes what is shown, and past the end is clamped rather than
	// blank.
	if Help(Default(), 100, 6, 3) == short {
		t.Error("scrolling did not move the window")
	}
	if got := Help(Default(), 100, 6, 9999); strings.TrimSpace(got) == "" {
		t.Error("scrolling past the end emptied the overlay")
	}
}

// A rebound action is styled differently from one still on its default, so it
// can be spotted at a glance. Two adjacent actions are used so both rows are
// inside the drawn window whichever one the cursor is on — and the cursor is
// moved off the rebound one, because the selected row has a style of its own
// that would mask the difference.
func TestEditorViewMarksAReboundRowThatIsNotSelected(t *testing.T) {
	isolate(t)
	neighbour := specs[1].id

	e, keys := at(t, NewEditor(), neighbour), Default()
	e, keys, _ = e.Update(press("enter"), keys)
	e, keys, _ = e.Update(press("z"), keys)
	if status, refused := e.Status(); refused {
		t.Fatalf("setup: could not rebind %s: %s", neighbour, status)
	}

	plain := NewEditor().View(Default(), 100, 30)
	rebound := e.MoveTo(0).View(keys, 100, 30) // cursor on the row above
	if plain == rebound {
		t.Error("a rebound row renders the same as a default one")
	}
	if !strings.Contains(rebound, "z") {
		t.Errorf("the rebound key is not shown:\n%s", rebound)
	}
}

// A refusal is styled as an error rather than a note, so it does not read as
// "done".
func TestEditorViewStylesARefusalDifferentlyFromANote(t *testing.T) {
	isolate(t)
	keys := Default()
	viewKey := bound(t, keys, "view")[0]

	// A note: "already the default".
	note, keys, _ := at(t, NewEditor(), "copy").Update(press("d"), keys)

	// A refusal: taking a key that is already spoken for.
	refused := at(t, NewEditor(), "copy")
	refused, keys, _ = refused.Update(press("enter"), keys)
	refused, keys, _ = refused.Update(press(viewKey), keys)

	if _, isFault := note.Status(); isFault {
		t.Fatal("setup: the note was recorded as a refusal")
	}
	if _, isFault := refused.Status(); !isFault {
		t.Fatal("setup: the refusal was recorded as a note")
	}
	if note.View(keys, 100, 30) == refused.View(keys, 100, 30) {
		t.Error("a refusal renders identically to a note")
	}
}

// An override in the config replaces the default rather than adding to it.
func TestFromAppliesAnOverride(t *testing.T) {
	keys := From(map[string][]string{"copy": {"z", "Z"}})

	if got := bound(t, keys, "copy"); !sameKeys(got, []string{"z", "Z"}) {
		t.Errorf("copy = %v, want the override [z Z]", got)
	}
	// Everything not named keeps its default.
	if got, want := bound(t, keys, "view"), bound(t, Default(), "view"); !sameKeys(got, want) {
		t.Errorf("view = %v, want the untouched default %v", got, want)
	}
	if _, ok := keys.Overrides()["copy"]; !ok {
		t.Error("the override is not reported as one")
	}
}

func TestIsFunctionKey(t *testing.T) {
	for in, want := range map[string]bool{
		"f1": true, "f12": true,
		"foo": false, // an f followed by something that is not a number
		"f":   false, // too short
		"f1a": false, // digits then not
		"":    false,
		"g1":  false,
	} {
		if got := isFunctionKey(in); got != want {
			t.Errorf("isFunctionKey(%q) = %v, want %v", in, got, want)
		}
	}
}
