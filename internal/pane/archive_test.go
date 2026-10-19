package pane

import (
	"testing"

	"github.com/kiruva/tyr/internal/fileops"
)

// A member listed by a tool comes with no size. Showing it as "0 B" would say
// every file in the archive is empty; it shows none instead.
func TestVirtualEntriesShowOnlyKnownSizes(t *testing.T) {
	entries := virtualEntries([]fileops.Member{
		{Path: "known.txt", Size: 2048},
		{Path: "listed.txt", NoSize: true},
	}, "")

	got := map[string]Entry{}
	for _, e := range entries {
		got[e.Name] = e
	}
	if e := got["known.txt"]; !e.HasSize || e.Size != 2048 {
		t.Errorf("known.txt = %+v, want its size shown", e)
	}
	if e := got["listed.txt"]; e.HasSize {
		t.Errorf("listed.txt = %+v, want no size shown", e)
	}
}
