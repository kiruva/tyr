package fileops

import "testing"

// A tool with alternates resolves to whichever name is installed, in
// preference order; one without alternates resolves to itself or to nothing.
func TestResolveTool(t *testing.T) {
	tests := []struct {
		name      string
		bin       string
		installed []string
		want      string
	}{
		{"canonical name wins", "7z", []string{"7z", "7zz", "7za"}, "7z"},
		{"official build", "7z", []string{"7zz"}, "7zz"},
		{"reduced build", "7z", []string{"7za"}, "7za"},
		{"preference order", "7z", []string{"7za", "7zz"}, "7zz"},
		{"none installed", "7z", nil, ""},
		{"no alternates", "unzip", []string{"unzip"}, "unzip"},
		{"no alternates, missing", "unzip", []string{"7zz"}, ""},
		{"a variant is not a substitute for another tool", "zip", []string{"7zz"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			have := func(bin string) bool {
				for _, i := range tt.installed {
					if i == bin {
						return true
					}
				}
				return false
			}
			if got := resolveTool(tt.bin, have); got != tt.want {
				t.Fatalf("resolveTool(%q) = %q, want %q", tt.bin, got, tt.want)
			}
		})
	}
}
