package middleware

import "testing"

func TestExtractPayloadAttribution(t *testing.T) {
	cases := []struct {
		name           string
		payload        string
		wantDistinctID string
		wantProps      map[string]string
	}{
		{
			name:           "agent run keys on user_id",
			payload:        `{"user_id":"u1","project_id":"p1","job_id":"j1","message":"hi"}`,
			wantDistinctID: "u1",
			wantProps: map[string]string{
				"user_id":    "u1",
				"project_id": "p1",
				"job_id":     "j1",
			},
		},
		{
			name:           "org grace teardown keys on org_id",
			payload:        `{"org_id":"o1"}`,
			wantDistinctID: "o1",
			wantProps:      map[string]string{"org_id": "o1"},
		},
		{
			name:           "web deploy falls back to deployment_id",
			payload:        `{"deployment_id":"d1"}`,
			wantDistinctID: "d1",
			wantProps:      map[string]string{"deployment_id": "d1"},
		},
		{
			name:           "fly delete app falls back to runtime_app_id",
			payload:        `{"fly_app_name":"wemob-x","runtime_app_id":"r1"}`,
			wantDistinctID: "r1",
			wantProps: map[string]string{
				"fly_app_name":   "wemob-x",
				"runtime_app_id": "r1",
			},
		},
		{
			name:           "empty payload returns empty distinct id",
			payload:        ``,
			wantDistinctID: "",
			wantProps:      map[string]string{},
		},
		{
			name:           "non-json payload returns empty distinct id",
			payload:        `not-json`,
			wantDistinctID: "",
			wantProps:      map[string]string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotID, gotProps := extractPayloadAttribution([]byte(tc.payload))
			if gotID != tc.wantDistinctID {
				t.Errorf("distinct id: got %q, want %q", gotID, tc.wantDistinctID)
			}
			if len(gotProps) != len(tc.wantProps) {
				t.Errorf("props len: got %d (%v), want %d (%v)", len(gotProps), gotProps, len(tc.wantProps), tc.wantProps)
			}
			for k, v := range tc.wantProps {
				if gotProps[k] != v {
					t.Errorf("prop %q: got %q, want %q", k, gotProps[k], v)
				}
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("short: got %q, want %q", got, "short")
	}
	if got := truncate("0123456789abc", 10); got != "0123456789…" {
		t.Errorf("long: got %q, want %q", got, "0123456789…")
	}
}
