package app

import "testing"

func TestMoveCursorStaysOnTheList(t *testing.T) {
	cases := []struct {
		key        string
		from, last int
		want       int
		handled    bool
	}{
		{"down", 0, 4, 1, true},
		{"j", 4, 4, 4, true},
		{"up", 0, 4, 0, true},
		{"pgdown", 1, 4, 4, true},
		{"ctrl+u", 3, 4, 0, true},
		{"G", 0, 4, 4, true},
		{"g", 3, 4, 0, true},
		{"down", 0, -1, 0, true}, // an empty list keeps the cursor on row 0
		{"end", 0, -1, 0, true},
		{"x", 2, 4, 2, false},
	}
	for _, c := range cases {
		cur := c.from
		if got := moveCursor(&cur, c.key, c.last, 3); got != c.handled || cur != c.want {
			t.Errorf("moveCursor(%d, %q, last %d) = %d, %v; want %d, %v", c.from, c.key, c.last, cur, got, c.want, c.handled)
		}
	}
}
