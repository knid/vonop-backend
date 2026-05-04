package agent

import "testing"

// TestAnyBackendPath nails down the prefix rule the auto-deploy hook
// keys off of. A trailing-slash hit is the only positive — backend.md,
// my-backend/, etc. should NOT trigger a deploy because they're not
// inside the deployable backend/ subtree.
func TestAnyBackendPath(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want bool
	}{
		{"empty", nil, false},
		{"only mobile changes", []string{"mobile/app/index.tsx", "mobile/lib/api.ts"}, false},
		{"backend api route", []string{"backend/app/api/chat/route.ts"}, true},
		{"backend lib + mobile", []string{"mobile/app/screen.tsx", "backend/lib/wemob-ai.ts"}, true},
		{"backend package.json scaffold", []string{"backend/package.json"}, true},
		{"sibling-named directory", []string{"my-backend/foo.ts", "tests/backend-mock.ts"}, false},
		{"file named backend.md at root", []string{"backend.md"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := anyBackendPath(c.in)
			if got != c.want {
				t.Errorf("anyBackendPath(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
