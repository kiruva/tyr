package metastrip

import (
	"fmt"
	"testing"
)

// grid is a tiny picture: each cell names the stored pixel it came from.
type grid [][]string

// stored is 3 wide and 2 tall, with no symmetry to hide a mistake behind.
func stored() grid {
	return grid{{"a", "b", "c"}, {"d", "e", "f"}}
}

// display shows stored pixels the way the EXIF spec defines each value, by
// where the stored image's first row and first column end up — written out
// from the spec's table, not from the matrices orient.go uses.
func display(g grid, v int) grid {
	h, w := len(g), len(g[0])
	// rowSide/colSide: where stored row 0 and stored column 0 are seen.
	sides := map[int][2]string{
		1: {"top", "left"}, 2: {"top", "right"}, 3: {"bottom", "right"}, 4: {"bottom", "left"},
		5: {"left", "top"}, 6: {"right", "top"}, 7: {"right", "bottom"}, 8: {"left", "bottom"},
	}[v]
	rowSide, colSide := sides[0], sides[1]
	var out grid
	if rowSide == "top" || rowSide == "bottom" {
		out = make(grid, h)
		for r := range out {
			out[r] = make([]string, w)
		}
		for r := range h {
			for c := range w {
				vr, vc := r, c
				if rowSide == "bottom" {
					vr = h - 1 - r
				}
				if colSide == "right" {
					vc = w - 1 - c
				}
				out[vr][vc] = g[r][c]
			}
		}
		return out
	}
	// Rows become columns: the picture is seen w tall and h wide.
	out = make(grid, w)
	for r := range out {
		out[r] = make([]string, h)
	}
	for r := range h {
		for c := range w {
			vc, vr := r, c
			if rowSide == "right" {
				vc = h - 1 - r
			}
			if colSide == "bottom" {
				vr = w - 1 - c
			}
			out[vr][vc] = g[r][c]
		}
	}
	return out
}

// turn does to a picture on screen what each Turn says, pixel by pixel.
func turn(g grid, t Turn) grid {
	h, w := len(g), len(g[0])
	make2 := func(rows, cols int) grid {
		out := make(grid, rows)
		for r := range out {
			out[r] = make([]string, cols)
		}
		return out
	}
	var out grid
	switch t {
	case RotateRight:
		out = make2(w, h)
		for r := range h {
			for c := range w {
				out[c][h-1-r] = g[r][c]
			}
		}
	case RotateLeft:
		out = make2(w, h)
		for r := range h {
			for c := range w {
				out[w-1-c][r] = g[r][c]
			}
		}
	case Rotate180:
		out = make2(h, w)
		for r := range h {
			for c := range w {
				out[h-1-r][w-1-c] = g[r][c]
			}
		}
	case FlipHorizontal:
		out = make2(h, w)
		for r := range h {
			for c := range w {
				out[r][w-1-c] = g[r][c]
			}
		}
	case FlipVertical:
		out = make2(h, w)
		for r := range h {
			for c := range w {
				out[h-1-r][c] = g[r][c]
			}
		}
	}
	return out
}

// Turning an image whose orientation is v must show what turning the picture
// on screen shows, for every value and every turn.
func TestTurnedMatchesTheSpec(t *testing.T) {
	for v := 1; v <= 8; v++ {
		for _, tr := range []Turn{RotateRight, RotateLeft, Rotate180, FlipHorizontal, FlipVertical} {
			got := Turned(v, tr)
			want := fmt.Sprint(turn(display(stored(), v), tr))
			if fmt.Sprint(display(stored(), got)) != want {
				t.Errorf("Turned(%d, %v) = %d, which shows %v; want %v", v, tr, got, display(stored(), got), want)
			}
		}
	}
}

func TestTurnedTreatsMissingAsNormal(t *testing.T) {
	for _, v := range []int{0, -1, 9} {
		if got := Turned(v, RotateRight); got != 6 {
			t.Errorf("Turned(%d, RotateRight) = %d, want 6", v, got)
		}
	}
}

// The two halves of the test above check each other: the values that are a
// single turn of a normal image show what that turn shows.
func TestSpecTableAgreesWithTheTurns(t *testing.T) {
	for v, tr := range map[int]Turn{2: FlipHorizontal, 3: Rotate180, 4: FlipVertical, 6: RotateRight, 8: RotateLeft} {
		if got, want := fmt.Sprint(display(stored(), v)), fmt.Sprint(turn(stored(), tr)); got != want {
			t.Errorf("value %d shows %s, want %s (%v)", v, got, want, tr)
		}
	}
}
