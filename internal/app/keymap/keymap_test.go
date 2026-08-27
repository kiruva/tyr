package keymap

import "testing"

func TestKeyLabels(t *testing.T) {
	cases := map[string]string{
		"up": "↑", " ": "space", "f5": "F5", "shift+f8": "shift+F8",
		"pgdown": "pgdn", "ctrl+l": "ctrl+l", "backspace": "⌫",
	}
	for in, want := range cases {
		if got := prettyKey(in); got != want {
			t.Errorf("prettyKey(%q) = %q, want %q", in, got, want)
		}
	}
	if got := keyLabel([]string{"f8", "delete", "d"}); got != "F8/del" {
		t.Errorf("keyLabel = %q, want the first two", got)
	}
}

// Every action has a name and a default, and no two share a key out of the box.
func TestDefaultsAreConsistent(t *testing.T) {
	keys := Default()
	seen := map[string]string{}

	for _, spec := range specs {
		if spec.id == "" || spec.desc == "" || len(spec.def) == 0 {
			t.Errorf("incomplete action: %+v", spec)
		}
		for _, k := range keys.boundKeys(spec) {
			if other, taken := seen[k]; taken {
				t.Errorf("%q is bound to both %s and %s", k, other, spec.id)
			}
			seen[k] = spec.id
		}
	}
}
