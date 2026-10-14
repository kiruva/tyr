package metastrip

import (
	"bytes"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// JUMBF (ISO 19566-5) boxes carry C2PA manifests: a "jumb" superbox starts
// with a "jumd" description box holding its label, followed by content boxes
// ("cbor", "json", "bfdb"/"bidb" embedded files) or further superboxes.

type box struct {
	typ  string
	data []byte
}

func jumbfHeaderLen(b []byte) int {
	if len(b) >= 4 && binary.BigEndian.Uint32(b) == 1 {
		return 16
	}
	return 8
}

func readBoxes(b []byte) []box {
	var out []box
	for i := 0; i+8 <= len(b); {
		l := binary.BigEndian.Uint32(b[i:])
		hdr, n := 8, int(l)
		switch l {
		case 0: // to end of data
			n = len(b) - i
		case 1: // 64-bit length follows
			if i+16 > len(b) {
				return out
			}
			hdr = 16
			n = int(min(binary.BigEndian.Uint64(b[i+8:]), uint64(len(b))))
		}
		if n < hdr || i+n > len(b) || i+n < i {
			return out
		}
		out = append(out, box{string(b[i+4 : i+8]), b[i+hdr : i+n]})
		i += n
	}
	return out
}

// jumb splits a superbox into its label and its remaining child boxes.
func jumb(data []byte) (label string, kids []box) {
	boxes := readBoxes(data)
	if len(boxes) == 0 || boxes[0].typ != "jumd" {
		return "", boxes
	}
	d := boxes[0].data
	if len(d) > 17 && d[16]&0x02 != 0 { // 16-byte type UUID, toggles, label
		l, _, _ := bytes.Cut(d[17:], []byte{0})
		label = string(l)
	}
	return label, boxes[1:]
}

func describeJUMBF(b []byte) []field {
	var out []field
	for _, top := range readBoxes(b) {
		if top.typ != "jumb" {
			continue
		}
		label, kids := jumb(top.data)
		if label != "c2pa" {
			out = append(out, field{"JUMBFLabel", label})
			continue
		}
		out = append(out, field{"", "C2PA Content Credentials manifest store (signature not verified)"})
		for _, k := range kids {
			if k.typ != "jumb" {
				continue
			}
			ml, mkids := jumb(k.data)
			out = append(out, field{"Manifest", ml})
			out = append(out, describeManifest(mkids)...)
		}
	}
	if len(out) == 0 {
		return []field{{"", fmt.Sprintf("<%d bytes, unreadable JUMBF>", len(b))}}
	}
	return out
}

func describeManifest(kids []box) []field {
	var out []field
	for _, k := range kids {
		if k.typ != "jumb" {
			continue
		}
		label, content := jumb(k.data)
		switch {
		case label == "c2pa.assertions":
			for _, a := range content {
				if a.typ == "jumb" {
					al, ac := jumb(a.data)
					out = append(out, describeAssertion(al, ac)...)
				}
			}
		case strings.HasPrefix(label, "c2pa.claim"):
			out = append(out, describeClaim(contentValue(content))...)
		case label == "c2pa.signature":
			out = append(out, describeSignature(contentValue(content))...)
		default:
			out = append(out, field{"Box", label})
		}
	}
	return out
}

// contentValue decodes the first CBOR or JSON content box.
func contentValue(boxes []box) any {
	for _, b := range boxes {
		switch b.typ {
		case "cbor":
			if v, err := cborDecode(b.data); err == nil {
				return v
			}
		case "json":
			var v any
			if json.Unmarshal(b.data, &v) == nil {
				return v
			}
		}
	}
	return nil
}

func describeAssertion(label string, content []box) []field {
	switch {
	case strings.HasPrefix(label, "c2pa.thumbnail"):
		size, format := 0, ""
		for _, b := range content {
			switch b.typ {
			case "bidb":
				size = len(b.data)
			case "bfdb": // toggles, media type
				if len(b.data) > 1 {
					f, _, _ := bytes.Cut(b.data[1:], []byte{0})
					format = string(f)
				}
			}
		}
		return []field{{"Thumbnail", fmt.Sprintf("<%d bytes, %s> (%s)", size, format, label)}}
	case strings.HasPrefix(label, "c2pa.hash"):
		return []field{{"Assertion", label + " (binds manifest to the file bytes)"}}
	}

	v := contentValue(content)
	switch {
	case strings.HasPrefix(label, "c2pa.actions"):
		var out []field
		acts, _ := get(v, "actions").([]any)
		for _, a := range acts {
			out = append(out, field{"Action", str(get(a, "action"))})
			if sa := get(a, "softwareAgent"); sa != nil {
				if name := str(get(sa, "name")); name != "" {
					sa = strings.TrimSpace(name + " " + str(get(sa, "version")))
				}
				out = append(out, field{"SoftwareAgent", render(sa)})
			}
			if dst := str(get(a, "digitalSourceType")); dst != "" {
				out = append(out, field{"DigitalSourceType", dst})
			}
			if when := str(get(a, "when")); when != "" {
				out = append(out, field{"When", when})
			}
		}
		if len(out) > 0 {
			return out
		}
	case strings.HasPrefix(label, "c2pa.soft-binding"):
		return []field{{"SoftBinding", render(get(v, "alg"))}}
	case strings.HasPrefix(label, "c2pa.ingredient"):
		return []field{{"Ingredient", fmt.Sprintf("%s (%s, %s)",
			str(get(v, "dc:title")), str(get(v, "dc:format")), str(get(v, "relationship")))}}
	}
	return []field{{label, render(v)}}
}

func describeClaim(v any) []field {
	var out []field
	gen := str(get(v, "claim_generator"))
	info := get(v, "claim_generator_info")
	if arr, ok := info.([]any); ok && len(arr) > 0 {
		info = arr[0]
	}
	if name := str(get(info, "name")); name != "" {
		gen = strings.TrimSpace(name + " " + str(get(info, "version")))
	}
	for _, f := range []field{
		{"ClaimGenerator", gen},
		{"Title", str(get(v, "dc:title"))},
		{"Format", str(get(v, "dc:format"))},
		{"InstanceID", str(get(v, "instanceID"))},
	} {
		if f.Value != "" {
			out = append(out, f)
		}
	}
	return out
}

// describeSignature reads the signer certificate from a COSE_Sign1 structure.
func describeSignature(v any) []field {
	if t, ok := v.(cborTag); ok {
		v = t.Val
	}
	arr, ok := v.([]any)
	if !ok || len(arr) < 2 {
		return []field{{"Signature", "<unreadable>"}}
	}
	var headers []any
	if p, ok := arr[0].([]byte); ok {
		if ph, err := cborDecode(p); err == nil {
			headers = append(headers, ph)
		}
	}
	headers = append(headers, arr[1])
	for _, h := range headers {
		chain := get(h, uint64(33)) // x5chain
		if chain == nil {
			chain = get(h, "x5chain")
		}
		if list, ok := chain.([]any); ok && len(list) > 0 {
			chain = list[0]
		}
		der, ok := chain.([]byte)
		if !ok {
			continue
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return []field{{"Signer", "<unparseable certificate>"}}
		}
		return []field{
			{"Signer", cert.Subject.String()},
			{"SignerIssuer", cert.Issuer.String()},
			{"SignerValidity", cert.NotBefore.Format("2006-01-02") + " to " + cert.NotAfter.Format("2006-01-02")},
		}
	}
	return []field{{"Signature", "present, no certificate found"}}
}

// get looks up a key in a decoded CBOR or JSON map.
func get(v any, key any) any {
	switch m := v.(type) {
	case []cborKV:
		for _, kv := range m {
			if kv.K == key {
				return kv.V
			}
		}
	case map[string]any:
		if k, ok := key.(string); ok {
			return m[k]
		}
	}
	return nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// render formats a decoded CBOR or JSON value compactly.
func render(v any) string {
	var sb strings.Builder
	renderTo(&sb, v, 0)
	return sb.String()
}

func renderTo(sb *strings.Builder, v any, depth int) {
	if depth > 16 {
		sb.WriteString("...")
		return
	}
	quote := func(s string) {
		if depth == 0 {
			sb.WriteString(s)
		} else {
			sb.WriteString(strconv.Quote(s))
		}
	}
	switch t := v.(type) {
	case nil:
		sb.WriteString("null")
	case string:
		quote(t)
	case []byte:
		fmt.Fprintf(sb, "<%d bytes>", len(t))
	case float64:
		sb.WriteString(trimFloat(t))
	case cborTag:
		renderTo(sb, t.Val, depth)
	case []any:
		sb.WriteString("[")
		for i, e := range t {
			if i > 0 {
				sb.WriteString(", ")
			}
			renderTo(sb, e, depth+1)
		}
		sb.WriteString("]")
	case []cborKV:
		sb.WriteString("{")
		for i, kv := range t {
			if i > 0 {
				sb.WriteString(", ")
			}
			renderTo(sb, kv.K, depth+1)
			sb.WriteString(": ")
			renderTo(sb, kv.V, depth+1)
		}
		sb.WriteString("}")
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		sb.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(sb, "%q: ", k)
			renderTo(sb, t[k], depth+1)
		}
		sb.WriteString("}")
	default:
		fmt.Fprint(sb, t)
	}
}
