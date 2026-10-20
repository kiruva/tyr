package app

import "testing"

// A mode with no entry in modal() falls through to the panes: its keys would
// move the file cursor and nothing would be drawn for it. Every mode but the
// panes' own draws something or, for the two that live in a pane's address
// bar, has a field taking input.
func TestEveryModeHasAModal(t *testing.T) {
	for md := modeNormal + 1; md <= modeKeys; md++ {
		m := Model{mode: md}
		e := m.modal()
		if e.key == nil {
			t.Errorf("mode %d has no key handler", md)
		}
		if e.view == nil && e.focused == nil && e.tick == nil {
			t.Errorf("mode %d has no entry in modal()", md)
		}
	}
}
