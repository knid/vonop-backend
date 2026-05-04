package controllers

import (
	"encoding/json"
	"testing"
)

func TestResolveRuntimeVersion(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		appVersion string
		sdkVersion string
		want       string
	}{
		{"literal string", `"1.2.3"`, "9.9.9", "50.0.0", "1.2.3"},
		{"appVersion policy", `{"policy":"appVersion"}`, "1.2.3", "50.0.0", "1.2.3"},
		{"sdkVersion policy", `{"policy":"sdkVersion"}`, "1.2.3", "50.0.0", "50.0.0"},
		{"nativeVersion falls back to appVersion", `{"policy":"nativeVersion"}`, "1.2.3", "50.0.0", "1.2.3"},
		{"unknown policy", `{"policy":"weird"}`, "1.2.3", "50.0.0", ""},
		{"empty raw", ``, "1.2.3", "50.0.0", ""},
		{"invalid json", `{not json`, "1.2.3", "50.0.0", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := resolveRuntimeVersion(json.RawMessage(c.raw), c.appVersion, c.sdkVersion)
			if got != c.want {
				t.Errorf("resolveRuntimeVersion(%q, %q, %q) = %q, want %q",
					c.raw, c.appVersion, c.sdkVersion, got, c.want)
			}
		})
	}
}
