package service

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestListAgentSpecs(t *testing.T) {
	dir := t.TempDir()
	// top-level: два spec + лишний файл; дубль расширения (pi-x.yaml + pi-x.yml → один entry)
	for _, n := range []string{"pi-lead.yaml", "pi-worker.yml", "pi-x.yaml", "pi-x.yml", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("name: x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// подкаталог — не учитывается
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "inner.yaml"), []byte("name: i\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := &TeamService{SpecsDir: dir}
	got := s.ListAgentSpecs()
	want := []string{"pi-lead", "pi-worker", "pi-x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListAgentSpecs = %v, want %v", got, want)
	}

	// несуществующий каталог → пустой список, без паники
	s2 := &TeamService{SpecsDir: filepath.Join(dir, "nope")}
	if got := s2.ListAgentSpecs(); len(got) != 0 {
		t.Fatalf("missing dir: ListAgentSpecs = %v, want empty", got)
	}
}
