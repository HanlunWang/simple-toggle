package app

import "testing"

func TestParseDeepLink(t *testing.T) {
	ok := map[string]deepLink{
		"simpletoggle://toggle/keep-awake.sh":     {Verb: "toggle", Target: "keep-awake.sh"},
		"simpletoggle://ON/wifi":                  {Verb: "on", Target: "wifi"},
		"simpletoggle://scene/%E4%B8%93%E6%B3%A8": {Verb: "scene", Target: "专注"},
		"simpletoggle://panel":                    {Verb: "panel"},
		"simpletoggle://workbench/scenes/focus":   {Verb: "workbench", Section: "scenes", Target: "focus"},
		"simpletoggle://settings/ai":              {Verb: "settings", Section: "ai"},
	}
	for in, want := range ok {
		if got, err := parseDeepLink(in); err != nil || got != want {
			t.Errorf("%s → %+v %v, want %+v", in, got, err, want)
		}
	}
	for _, bad := range []string{"https://x/toggle/a", "simpletoggle://toggle", "simpletoggle://delete/x", "simpletoggle://run/a/b", "::"} {
		if _, err := parseDeepLink(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
