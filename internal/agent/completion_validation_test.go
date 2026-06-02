package agent

import (
	"strings"
	"testing"
)

const yachtSaaSPrompt = `Create a Yacht Logbook SaaS platform.
Admin/Yacht Owner can manage yachts. Captain can only see assigned yacht.
Include weekly status entry form, log history, smart alerts, and mobile-first dashboard.`

func TestValidateGeneratedAppRejectsMarketingBrochureForSaaSRequest(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"app/layout.tsx": "export default function Layout({children}) { return children }",
		"app/page.tsx":   "export default function Page(){ return <main>Hero Testimonials CTA Yacht showcase</main> }",
	})

	err := validateGeneratedApp(files, NewBuildState(yachtSaaSPrompt, true), yachtSaaSPrompt, true)
	if err == nil {
		t.Fatal("expected validation error")
	}
	msg := err.Error()
	for _, want := range []string{"captain interface", "data entry form", "log history", "smart alerts"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected %q in validation error, got: %s", want, msg)
		}
	}
}

func TestValidateGeneratedAppAllowsOperationalSaaSShape(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"app/layout.tsx": "export default function Layout({children}) { return children }",
		"app/page.tsx": `export default function Page(){
			return <main>
				<section>Admin fleet dashboard for yacht owners</section>
				<section>Captain assigned yacht interface and role access</section>
				<form><input /><textarea /><button>Submit weekly entry</button></form>
				<section>Log history with past chronological entries</section>
				<section>Smart alert threshold battery service warning</section>
			</main>
		}`,
	})

	if err := validateGeneratedApp(files, NewBuildState(yachtSaaSPrompt, true), yachtSaaSPrompt, true); err != nil {
		t.Fatalf("expected validation success, got: %v", err)
	}
}

func TestValidateGeneratedAppRejectsImagePlaceholders(t *testing.T) {
	files := NewSafeFileMap(map[string]string{
		"app/page.tsx": "const hero = '__IMG_HERO__'",
	})

	err := validateGeneratedApp(files, nil, "change image", false)
	if err == nil {
		t.Fatal("expected placeholder validation error")
	}
	if !strings.Contains(err.Error(), "app/page.tsx") {
		t.Fatalf("expected path in error, got: %v", err)
	}
}
