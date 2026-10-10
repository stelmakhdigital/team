package service

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// ---------- P2.7: transcript search — чистые функции (матч/окно/merge/bounded) ----------

func TestTranscriptWindowsBasic(t *testing.T) {
	lines := []string{"a", "b", "the target is here", "d", "e", "f", "g"}
	wins := transcriptWindows(lines, "target", transcriptContext, transcriptMergeGap)
	if len(wins) != 1 {
		t.Fatalf("windows = %d, want 1", len(wins))
	}
	// match на строке 2 → окно [2-3, 2+3] = [-1→0, 5]
	if wins[0].start != 0 || wins[0].end != 5 {
		t.Fatalf("window = [%d,%d], want [0,5]", wins[0].start, wins[0].end)
	}
	if wins[0].primary != 2 {
		t.Fatalf("primary = %d, want 2", wins[0].primary)
	}
}

func TestTranscriptWindowsCaseInsensitive(t *testing.T) {
	lines := []string{"hello World", "foo"}
	wins := transcriptWindows(lines, "wOrLd", transcriptContext, transcriptMergeGap)
	if len(wins) != 1 || wins[0].primary != 0 {
		t.Fatalf("case-insensitive match failed: %v", wins)
	}
}

func TestTranscriptWindowsMerge(t *testing.T) {
	// два совпадения близко (<= mergeGap между окнами) → слияние в одно окно
	lines := []string{"x", "needle", "y", "needle", "z"}
	wins := transcriptWindows(lines, "needle", transcriptContext, transcriptMergeGap)
	if len(wins) != 1 {
		t.Fatalf("merged windows = %d, want 1: %v", len(wins), wins)
	}
	// primary — первое совпадение
	if wins[0].primary != 1 {
		t.Fatalf("primary = %d, want 1", wins[0].primary)
	}
}

func TestTranscriptWindowsSeparate(t *testing.T) {
	// два совпадения далеко (окна не пересекаются + > mergeGap) → два окна
	// ctx=3: окно [m-3, m+3]; слияние если start2 <= end1+3 → m2-m1 <= 9
	lines := make([]string, 14)
	for i := range lines {
		lines[i] = "filler"
	}
	lines[0] = "needle"
	lines[13] = "needle"
	wins := transcriptWindows(lines, "needle", transcriptContext, transcriptMergeGap)
	if len(wins) != 2 {
		t.Fatalf("windows = %d, want 2: %v", len(wins), wins)
	}
}

func TestTranscriptWindowsNoMatch(t *testing.T) {
	lines := []string{"a", "b", "c"}
	if wins := transcriptWindows(lines, "zzz", transcriptContext, transcriptMergeGap); len(wins) != 0 {
		t.Fatalf("expected no windows, got %v", wins)
	}
}

func TestTranscriptWindowsContextClamp(t *testing.T) {
	// совпадение в первой строке → окно не уходит < 0
	lines := []string{"match", "b", "c"}
	wins := transcriptWindows(lines, "match", transcriptContext, transcriptMergeGap)
	if len(wins) != 1 || wins[0].start != 0 {
		t.Fatalf("clamp failed: %v", wins)
	}
}

func TestReadTailLinesFewer(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "t.log")
	if err := os.WriteFile(p, []byte("1\n2\n3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 || lines[0] != "1" || lines[2] != "3" {
		t.Fatalf("tail (fewer) = %v", lines)
	}
}

func TestReadTailLinesBounded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.log")
	// 100 строк; tail 5 → последние 5 (96..100)
	var b []byte
	for i := 1; i <= 100; i++ {
		b = append(b, []byte("l"+strconv.Itoa(i)+"\n")...)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	lines, err := readTailLines(p, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 5 {
		t.Fatalf("tail = %d lines, want 5", len(lines))
	}
	if lines[0] != "l96" || lines[4] != "l100" {
		t.Fatalf("tail wrong: first=%q last=%q, want l96..l100", lines[0], lines[4])
	}
}

func TestReadTailLinesMissing(t *testing.T) {
	if _, err := readTailLines(filepath.Join(t.TempDir(), "nope.log"), 10); err == nil {
		t.Fatal("expected error for missing file")
	}
}
