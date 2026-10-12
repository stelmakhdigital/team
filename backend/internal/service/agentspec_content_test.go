package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGetAgentSpecContent — W3.2: содержимое agent spec (read-only).
// 4 сценария: normal, 404, traversal (a/b, ../etc/passwd), большой файл (>100KB).
func TestGetAgentSpecContent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pi-lead.yaml"), []byte("name: pi-lead\nmodel: claude-3-7-sonnet\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pi-worker.yml"), []byte("name: pi-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &TeamService{SpecsDir: dir}

	// (a) normal: .yaml
	got, err := s.GetAgentSpecContent("pi-lead")
	if err != nil {
		t.Fatalf("normal .yaml: %v", err)
	}
	if !strings.Contains(got, "name: pi-lead") || !strings.Contains(got, "model: claude-3-7-sonnet") {
		t.Errorf("content = %q, want pi-lead yaml", got)
	}
	// (a2) normal: .yml
	got, err = s.GetAgentSpecContent("pi-worker")
	if err != nil {
		t.Fatalf("normal .yml: %v", err)
	}
	if !strings.Contains(got, "name: pi-worker") {
		t.Errorf("content = %q, want pi-worker yaml", got)
	}

	// (b) 404: не существует
	_, err = s.GetAgentSpecContent("nope")
	if err == nil {
		t.Fatal("missing spec: want 404, got nil")
	}
	ae, ok := err.(*AppError)
	if !ok || ae.Status != 404 {
		t.Fatalf("missing spec: want status 404, got %v", err)
	}

	// (c) traversal: разделители и '..' → 400 (не читает вне SpecsDir)
	for _, bad := range []string{"a/b", "../etc/passwd", "..%2Fetc%2Fpasswd", "a\\b", "/etc/passwd", "..", ".", ""} {
		_, err = s.GetAgentSpecContent(bad)
		if err == nil {
			t.Fatalf("traversal %q: want error, got nil", bad)
			continue
		}
		ae, ok := err.(*AppError)
		if !ok || ae.Status != 400 {
			t.Errorf("traversal %q: want status 400, got %v", bad, err)
		}
	}

	// (d) большой файл (> 100 KB) → 400 (лимит)
	big := strings.Repeat("x", 100*1024+1)
	if err := os.WriteFile(filepath.Join(dir, "huge.yaml"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetAgentSpecContent("huge")
	if err == nil {
		t.Fatal("huge spec: want 400 (limit), got nil")
	}
	ae2, ok2 := err.(*AppError)
	if !ok2 || ae2.Status != 400 {
		t.Fatalf("huge spec: want status 400, got %v", err)
	}
}
