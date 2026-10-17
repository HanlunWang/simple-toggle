package keys

import "testing"

func TestParse(t *testing.T) {
	cases := map[string]string{
		"f15": "f15", " F15 ": "f15", "shift+cmd+k": "cmd+shift+k", "alt+control+space": "opt+ctrl+space",
		"command+option+left": "cmd+opt+left", "mouse:middle": "mouse:middle",
	}
	for in, want := range cases {
		s, ok := Parse(in)
		if !ok || s.Canonical() != want {
			t.Errorf("Parse(%q) = %q %v, want %q", in, s.Canonical(), ok, want)
		}
	}
	for _, bad := range []string{"", "hyper+k", "cmd+", "f99", "mouse:back", "cmd+shift"} {
		if _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
	s, _ := Parse("cmd+shift+k")
	if s.Code != 40 || s.Flags() != FlagCommand|FlagShift || s.Display() != "⌘⇧K" || s.BareCharacter() {
		t.Errorf("spec %+v display %q", s, s.Display())
	}
	if s, _ := Parse("k"); !s.BareCharacter() {
		t.Error("bare k")
	}
	if s, _ := Parse("mouse:right"); s.Target != TargetMouse || s.Button != MouseRight || s.Display() != "" {
		t.Errorf("mouse %+v", s)
	}
}
