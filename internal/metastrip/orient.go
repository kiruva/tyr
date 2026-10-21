package metastrip

// An image's EXIF orientation says how the stored pixels are to be shown:
// one of eight values, every combination of a quarter turn and a mirror.
// Turning or flipping a picture is then a matter of changing that value — the
// pixels stay exactly as they are, which is what makes it lossless.
//
// Each value is a 2×2 matrix on screen coordinates (x right, y down) taking a
// stored pixel's position to where it is seen. Doing something to the picture
// on screen is one more matrix applied after it, and the product is the new
// value. The eight are a closed group, so the product is always one of them.

// Turn is one thing done to a picture as it is seen.
type Turn int

const (
	RotateRight    Turn = iota + 1 // a quarter turn clockwise
	RotateLeft                     // a quarter turn anticlockwise
	Rotate180                      // a half turn
	FlipHorizontal                 // mirrored left to right
	FlipVertical                   // mirrored top to bottom
)

func (t Turn) String() string {
	switch t {
	case RotateRight:
		return "rotate right"
	case RotateLeft:
		return "rotate left"
	case Rotate180:
		return "rotate 180°"
	case FlipHorizontal:
		return "flip horizontally"
	case FlipVertical:
		return "flip vertically"
	}
	return "no change"
}

// matrix maps (x, y) to (a·x + b·y, c·x + d·y).
type matrix struct{ a, b, c, d int }

func (m matrix) then(n matrix) matrix {
	// n applied after m: n·m.
	return matrix{
		n.a*m.a + n.b*m.c, n.a*m.b + n.b*m.d,
		n.c*m.a + n.d*m.c, n.c*m.b + n.d*m.d,
	}
}

// orientations are the eight EXIF values, index = value. Each is "mirror
// horizontally, then rotate" as the spec's names read, worked out once:
// 5 is the transpose and 7 the transverse.
var orientations = [...]matrix{
	1: {1, 0, 0, 1},   // normal
	2: {-1, 0, 0, 1},  // mirror horizontal
	3: {-1, 0, 0, -1}, // rotate 180
	4: {1, 0, 0, -1},  // mirror vertical
	5: {0, 1, 1, 0},   // mirror horizontal, rotate 270 CW
	6: {0, -1, 1, 0},  // rotate 90 CW
	7: {0, -1, -1, 0}, // mirror horizontal, rotate 90 CW
	8: {0, 1, -1, 0},  // rotate 270 CW
}

var turns = map[Turn]matrix{
	RotateRight:    orientations[6],
	RotateLeft:     orientations[8],
	Rotate180:      orientations[3],
	FlipHorizontal: orientations[2],
	FlipVertical:   orientations[4],
}

// Turned is the orientation an image has after t, given the one it has now. A
// value outside 1–8 — including 0, for an image that has none — is taken as
// normal, which is how every viewer shows such an image.
func Turned(orientation int, t Turn) int {
	if orientation < 1 || orientation > 8 {
		orientation = 1
	}
	m, ok := turns[t]
	if !ok {
		return orientation
	}
	want := orientations[orientation].then(m)
	for v := 1; v <= 8; v++ {
		if orientations[v] == want {
			return v
		}
	}
	return orientation // unreachable: the eight are closed under composition
}

// OrientationName says in words how an orientation value shows the image.
func OrientationName(v int) string {
	if v < 1 || v > 8 {
		return orientationNames[1]
	}
	return orientationNames[v]
}
