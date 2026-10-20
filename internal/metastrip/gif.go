package metastrip

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

func stripGIF(b []byte, o Options) (*Result, error) {
	r := &Result{}
	if len(b) < 13 {
		return nil, errors.New("gif: truncated header")
	}
	i := 13
	if b[10]&0x80 != 0 { // global color table
		i += 3 << (b[10]&7 + 1)
	}
	if i > len(b) {
		return nil, errors.New("gif: truncated color table")
	}
	out := append(make([]byte, 0, len(b)), b[:i]...)
	for {
		if i >= len(b) {
			return nil, errors.New("gif: missing trailer")
		}
		switch b[i] {
		case 0x3B: // trailer
			out = append(out, 0x3B)
			if extra := len(b) - i - 1; extra > 0 {
				r.add("data after trailer", extra, "trailer", b[i+1:])
			}
			r.Data = out
			return r, nil

		case 0x2C: // image descriptor
			if i+10 > len(b) {
				return nil, errors.New("gif: truncated image descriptor")
			}
			j := i + 10
			if b[i+9]&0x80 != 0 { // local color table
				j += 3 << (b[i+9]&7 + 1)
			}
			end, err := skipSubBlocks(b, j+1) // +1: LZW minimum code size
			if err != nil {
				return nil, err
			}
			out = append(out, b[i:end]...)
			i = end

		case 0x21: // extension
			if i+2 > len(b) {
				return nil, errors.New("gif: truncated extension")
			}
			end, err := skipSubBlocks(b, i+2)
			if err != nil {
				return nil, err
			}
			if it, drop := gifClassify(b[i+1], b[i+2:end], o); drop {
				r.add(it.Name, end-i, it.Kind, it.Data)
			} else {
				out = append(out, b[i:end]...)
			}
			i = end

		default:
			return nil, fmt.Errorf("gif: unexpected block 0x%02x at offset %d", b[i], i)
		}
	}
}

func skipSubBlocks(b []byte, i int) (int, error) {
	for {
		if i >= len(b) {
			return 0, errors.New("gif: truncated data block")
		}
		n := int(b[i])
		i += 1 + n
		if n == 0 {
			return i, nil
		}
	}
}

// gifClassify decides whether an extension block is metadata to drop and, if
// it is, what it is called and what of it -i can decode (Size is left to the
// caller). blocks holds the extension's sub-blocks, starting with the first
// size byte.
func gifClassify(label byte, blocks []byte, o Options) (it Item, drop bool) {
	switch label {
	case 0xF9, 0x01: // graphic control, plain text (rendered content)
		return Item{}, false
	case 0xFE:
		return Item{Name: "comment extension", Kind: "comment", Data: flattenSubBlocks(blocks)}, true
	case 0xFF:
		if len(blocks) < 12 || blocks[0] != 11 {
			return Item{Name: "application extension"}, true
		}
		id := string(blocks[1:12])
		if strings.HasPrefix(id, "C2PA") {
			return Item{Name: "C2PA application extension", Kind: "jumbf", Data: flattenSubBlocks(blocks[12:])}, true
		}
		switch id {
		case "NETSCAPE2.0", "ANIMEXTS1.0": // loop count
			return Item{}, false
		case "XMP DataXMP":
			// XMP is stored raw (its bytes double as sub-block sizes), followed
			// by a "magic trailer"; cut at the end of the XMP packet.
			x := blocks[12:]
			if k := bytes.Index(x, []byte("<?xpacket end")); k >= 0 {
				if e := bytes.Index(x[k:], []byte("?>")); e >= 0 {
					x = x[:k+e+2]
				}
			}
			return Item{Name: "XMP application extension", Kind: "xmp", Data: x}, true
		case "ICCRGBG1012":
			return Item{Name: "ICC profile application extension", Kind: "icc", Data: flattenSubBlocks(blocks[12:])}, !o.KeepICC
		default:
			return Item{Name: fmt.Sprintf("application extension %q", id)}, true
		}
	}
	return Item{Name: fmt.Sprintf("extension 0x%02x", label)}, true
}

func flattenSubBlocks(b []byte) []byte {
	var out []byte
	for i := 0; i < len(b) && b[i] != 0; {
		n := int(b[i])
		end := min(i+1+n, len(b))
		out = append(out, b[i+1:end]...)
		i = end
	}
	return out
}
