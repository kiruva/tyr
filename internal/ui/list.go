package ui

// MoveCursor applies the keys every scrolling list shares — ↑↓ and jk, page
// up and down, home and end — to a cursor over rows 0..last, page rows at a
// time. It reports whether key was one of them.
func MoveCursor(cursor *int, key string, last, page int) bool {
	c := *cursor
	switch key {
	case "up", "k":
		c--
	case "down", "j":
		c++
	case "pgup", "ctrl+u":
		c -= page
	case "pgdown", "ctrl+d":
		c += page
	case "home", "g":
		c = 0
	case "end", "G":
		c = last
	default:
		return false
	}
	*cursor = max(min(c, last), 0)
	return true
}
