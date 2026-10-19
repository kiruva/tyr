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
		if spec.id == "" || spec.desc == "" || (len(spec.def) == 0 && spec.id != "capabilities") {
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

// Ctrl+C and Esc belong to the app, not to any one action: none ships bound to
// either, and a config file that binds one gets it dropped. An override left
// with nothing in it falls back to the default.
func TestReservedKeysAreNeverBound(t *testing.T) {
	for _, spec := range specs {
		for _, k := range spec.def {
			if reservedKeys[k] {
				t.Errorf("%s ships bound to reserved %s", spec.id, k)
			}
		}
	}

	keys := From(map[string][]string{"quit": {"Q", "ctrl+c"}, "copy": {"esc"}})
	if got := keys.boundKeys(mustSpec(t, "quit")); !sameKeys(got, []string{"Q"}) {
		t.Errorf("quit = %v, want [Q]", got)
	}
	copySpec := mustSpec(t, "copy")
	if got := keys.boundKeys(copySpec); !sameKeys(got, copySpec.def) {
		t.Errorf("copy = %v, want the default %v", got, copySpec.def)
	}
}

func mustSpec(t *testing.T, id string) Spec {
	t.Helper()
	spec, ok := specByID(id)
	if !ok {
		t.Fatalf("no action %q", id)
	}
	return spec
}
