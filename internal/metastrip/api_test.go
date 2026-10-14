package metastrip

import (
	"strings"
	"testing"
)

func TestInspectReport(t *testing.T) {
	in := dirtyJPEG(t)
	if Detect(in) != "jpeg" {
		t.Fatalf("Detect = %q", Detect(in))
	}
	res, err := Strip(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	rep := Inspect(res)
	if len(rep.Sections) != len(res.Removed) {
		t.Fatalf("%d sections for %d items", len(rep.Sections), len(res.Removed))
	}
	total := 0
	for _, it := range res.Removed {
		total += it.Size
	}
	if rep.Total != total {
		t.Errorf("Total = %d, want %d", rep.Total, total)
	}
	found := false
	for _, s := range rep.Sections {
		for _, f := range s.Fields {
			if f.Key == "Orientation" && strings.HasPrefix(f.Value, "6") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("no Orientation field in %+v", rep.Sections)
	}
}

func TestInspectEscapes(t *testing.T) {
	var d []byte
	d = append(d, 0xFF, 0xD8)
	d = append(d, jpegSeg(0xFE, []byte("evil\x1b[31mred"))...)
	d = append(d, dirtyJPEG(t)[2:]...)
	res, err := Strip(d, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range Inspect(res).Sections {
		for _, f := range s.Fields {
			if strings.ContainsRune(f.Value, 0x1b) {
				t.Fatalf("escape passed through: %q", f.Value)
			}
		}
	}
}

func TestUnsupportedNamed(t *testing.T) {
	heic := []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00")
	if Detect(heic) != "" || Unsupported(heic) != "HEIC/AVIF" {
		t.Errorf("Detect=%q Unsupported=%q", Detect(heic), Unsupported(heic))
	}
}
