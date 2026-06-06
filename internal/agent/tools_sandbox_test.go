package agent

import "testing"

func TestValidateSandboxWritePathArtifactsOnly(t *testing.T) {
	if _, err := validateSandboxWritePath("/home/user/artifacts/run/report.md"); err != nil {
		t.Fatalf("artifact path rejected: %v", err)
	}
	if _, err := validateSandboxWritePath("/tmp/report.md"); err != nil {
		t.Fatalf("tmp path rejected: %v", err)
	}
	if _, err := validateSandboxWritePath("/home/user/project/app/page.tsx"); err == nil {
		t.Fatal("expected project source write to be rejected")
	}
	if _, err := validateSandboxWritePath("/home/user/artifacts/.env"); err == nil {
		t.Fatal("expected secret-looking artifact path to be rejected")
	}
}

func TestValidateSandboxReadPathBlocksSecretsAndOutsideRoots(t *testing.T) {
	if _, err := validateSandboxReadPath("/home/user/project/package.json"); err != nil {
		t.Fatalf("project read rejected: %v", err)
	}
	if _, err := validateSandboxReadPath("/home/user/project/.env.local"); err == nil {
		t.Fatal("expected env read to be rejected")
	}
	if _, err := validateSandboxReadPath("/etc/passwd"); err == nil {
		t.Fatal("expected outside root read to be rejected")
	}
}

func TestValidateSandboxCommandBlocksRiskyCommands(t *testing.T) {
	if err := validateSandboxCommand("npm test"); err != nil {
		t.Fatalf("safe command rejected: %v", err)
	}
	for _, cmd := range []string{
		"sudo whoami",
		"apt-get update",
		"rm -rf /",
		"rm -rf /*",
		"cat /home/user/project/.env.local",
		"ngrok http 3000",
	} {
		if err := validateSandboxCommand(cmd); err == nil {
			t.Fatalf("expected command %q to be blocked", cmd)
		}
	}
}

func TestAllowedPreviewURL(t *testing.T) {
	if err := allowedPreviewURL("https://3000-sbx.preview.wemob.app/report.pdf"); err != nil {
		t.Fatalf("preview URL rejected: %v", err)
	}
	if err := allowedPreviewURL("https://3000-sbx.e2b.app/report.pdf"); err != nil {
		t.Fatalf("e2b URL rejected: %v", err)
	}
	if err := allowedPreviewURL("https://example.com/report.pdf"); err == nil {
		t.Fatal("expected external URL to be rejected")
	}
	if err := allowedPreviewURL("http://3000-sbx.preview.wemob.app/report.pdf"); err == nil {
		t.Fatal("expected non-https URL to be rejected")
	}
}
