package gitrepo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readTarGz decompresses gzipped tar bytes into a name -> content map.
func readTarGz(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %q: %v", hdr.Name, err)
		}
		out[hdr.Name] = buf
	}
	return out
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", path, err)
	}
}

func mustWrite(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir parent of %q: %v", path, err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func TestTarSubtree_StripsPrefixAndPreservesContent(t *testing.T) {
	dir := t.TempDir()
	indexContent := []byte("export default function Index() { return null }\n")
	apiContent := []byte("export const api = {}\n")
	iconContent := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x01, 0x02, 0x03, 0xff, 0xfe}
	routeContent := []byte("export function GET() {}\n")

	mustWrite(t, filepath.Join(dir, "mobile/app/index.tsx"), indexContent)
	mustWrite(t, filepath.Join(dir, "mobile/lib/api.ts"), apiContent)
	mustWrite(t, filepath.Join(dir, "mobile/assets/icon.png"), iconContent)
	mustWrite(t, filepath.Join(dir, "backend/route.ts"), routeContent)

	r := &Repo{Path: dir}
	data, err := r.TarSubtree("mobile/")
	if err != nil {
		t.Fatalf("TarSubtree: %v", err)
	}

	entries := readTarGz(t, data)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(entries), keysOf(entries))
	}
	if got, want := entries["app/index.tsx"], indexContent; !bytes.Equal(got, want) {
		t.Errorf("app/index.tsx content mismatch: got %q want %q", got, want)
	}
	if got, want := entries["lib/api.ts"], apiContent; !bytes.Equal(got, want) {
		t.Errorf("lib/api.ts content mismatch: got %q want %q", got, want)
	}
	if got, want := entries["assets/icon.png"], iconContent; !bytes.Equal(got, want) {
		t.Errorf("assets/icon.png content mismatch: got %v want %v", got, want)
	}
	for name := range entries {
		if strings.HasPrefix(name, "backend/") || strings.HasPrefix(name, "mobile/") {
			t.Errorf("unexpected entry %q (should not include backend/ or mobile/ prefix)", name)
		}
	}
}

func TestTarSubtree_ExcludesGitDir(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "mobile/.git/HEAD"), []byte("ref: refs/heads/main\n"))
	mustWrite(t, filepath.Join(dir, "mobile/.git/objects/abcd"), []byte("blob"))
	mustWrite(t, filepath.Join(dir, "mobile/index.tsx"), []byte("hello"))

	r := &Repo{Path: dir}
	data, err := r.TarSubtree("mobile/")
	if err != nil {
		t.Fatalf("TarSubtree: %v", err)
	}

	entries := readTarGz(t, data)
	if _, ok := entries["index.tsx"]; !ok {
		t.Errorf("expected index.tsx in tar, got entries: %v", keysOf(entries))
	}
	for name := range entries {
		if strings.HasPrefix(name, ".git/") || name == ".git/HEAD" {
			t.Errorf("unexpected .git entry %q in tar", name)
		}
	}
}

func TestTarSubtree_RejectsBadPrefix(t *testing.T) {
	dir := t.TempDir()
	r := &Repo{Path: dir}

	cases := []struct {
		name      string
		prefix    string
		substring string
	}{
		{"empty", "", "invalid prefix"},
		{"dotdot", "..", "invalid prefix"},
		{"dotdot escape", "../../etc", "invalid prefix"},
		{"nonexistent", "nonexistent/", "stat"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := r.TarSubtree(tc.prefix)
			if err == nil {
				t.Fatalf("expected error for prefix %q, got nil", tc.prefix)
			}
			if !strings.Contains(err.Error(), tc.substring) {
				t.Errorf("expected error to contain %q, got: %v", tc.substring, err)
			}
		})
	}
}

func TestTarSubtree_FailsOnFilePrefix(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "mobile"), []byte("not a directory"))

	r := &Repo{Path: dir}
	_, err := r.TarSubtree("mobile")
	if err == nil {
		t.Fatal("expected error when prefix points to a file, got nil")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("expected error to contain 'not a directory', got: %v", err)
	}
}

func TestTarSubtree_EmptySubtreeIsValidGzip(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "mobile"))

	r := &Repo{Path: dir}
	data, err := r.TarSubtree("mobile")
	if err != nil {
		t.Fatalf("TarSubtree: %v", err)
	}

	entries := readTarGz(t, data)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty subtree, got %d: %v", len(entries), keysOf(entries))
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
