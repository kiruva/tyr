package metastrip

import (
	"encoding/binary"
	"errors"
	"math"
)

// Minimal CBOR (RFC 8949) decoder, enough to read C2PA manifests.
// Maps decode to []cborKV to keep order and allow non-string keys.

type cborKV struct{ K, V any }

type cborTag struct {
	Num uint64
	Val any
}

var errCBOR = errors.New("invalid CBOR")

func cborDecode(b []byte) (any, error) {
	v, _, err := cborItem(b, 0)
	return v, err
}

func cborItem(b []byte, depth int) (any, []byte, error) {
	if depth > 64 || len(b) == 0 {
		return nil, nil, errCBOR
	}
	major, ai := b[0]>>5, b[0]&31
	b = b[1:]
	var n uint64
	indef := false
	switch {
	case ai < 24:
		n = uint64(ai)
	case ai <= 27:
		size := 1 << (ai - 24)
		if len(b) < size {
			return nil, nil, errCBOR
		}
		switch size {
		case 1:
			n = uint64(b[0])
		case 2:
			n = uint64(binary.BigEndian.Uint16(b))
		case 4:
			n = uint64(binary.BigEndian.Uint32(b))
		case 8:
			n = binary.BigEndian.Uint64(b)
		}
		b = b[size:]
	case ai == 31 && major >= 2 && major <= 5:
		indef = true
	default:
		return nil, nil, errCBOR
	}

	switch major {
	case 0:
		return n, b, nil
	case 1:
		if n > math.MaxInt64 {
			return -1 - float64(n), b, nil
		}
		return -1 - int64(n), b, nil
	case 2, 3:
		var s []byte
		if indef {
			for {
				if len(b) == 0 {
					return nil, nil, errCBOR
				}
				if b[0] == 0xFF {
					b = b[1:]
					break
				}
				chunk, rest, err := cborItem(b, depth+1)
				if err != nil {
					return nil, nil, err
				}
				switch c := chunk.(type) {
				case []byte:
					s = append(s, c...)
				case string:
					s = append(s, c...)
				default:
					return nil, nil, errCBOR
				}
				b = rest
			}
		} else {
			if n > uint64(len(b)) {
				return nil, nil, errCBOR
			}
			s, b = b[:n], b[n:]
		}
		if major == 3 {
			return string(s), b, nil
		}
		return s, b, nil
	case 4, 5:
		if !indef && n > uint64(len(b)) { // every item takes at least one byte
			return nil, nil, errCBOR
		}
		var arr []any
		var m []cborKV
		for i := uint64(0); indef || i < n; i++ {
			if indef {
				if len(b) == 0 {
					return nil, nil, errCBOR
				}
				if b[0] == 0xFF {
					b = b[1:]
					break
				}
			}
			v, rest, err := cborItem(b, depth+1)
			if err != nil {
				return nil, nil, err
			}
			b = rest
			if major == 4 {
				arr = append(arr, v)
				continue
			}
			val, rest, err := cborItem(b, depth+1)
			if err != nil {
				return nil, nil, err
			}
			b = rest
			m = append(m, cborKV{v, val})
		}
		if major == 4 {
			return arr, b, nil
		}
		return m, b, nil
	case 6:
		v, rest, err := cborItem(b, depth+1)
		if err != nil {
			return nil, nil, err
		}
		return cborTag{n, v}, rest, nil
	}
	// major 7: simple values and floats
	switch ai {
	case 20:
		return false, b, nil
	case 21:
		return true, b, nil
	case 22, 23:
		return nil, b, nil
	case 25:
		return halfFloat(uint16(n)), b, nil
	case 26:
		return float64(math.Float32frombits(uint32(n))), b, nil
	case 27:
		return math.Float64frombits(n), b, nil
	}
	return n, b, nil
}

func halfFloat(h uint16) float64 {
	exp, frac := int(h>>10&0x1F), float64(h&0x3FF)
	var v float64
	switch exp {
	case 0:
		v = math.Ldexp(frac, -24)
	case 31:
		v = math.Inf(1)
		if frac != 0 {
			v = math.NaN()
		}
	default:
		v = math.Ldexp(frac+1024, exp-25)
	}
	if h&0x8000 != 0 {
		v = -v
	}
	return v
}
