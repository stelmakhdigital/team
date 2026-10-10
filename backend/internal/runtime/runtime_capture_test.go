package runtime

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- P2.8: Capture (runtime-адаптеры) ----------

func TestProcessCaptureNoScreen(t *testing.T) {
	a := &ProcessAdapter{}
	_, err := a.Capture(context.Background(), "ref", 10)
	if !errors.Is(err, ErrNoScreen) {
		t.Errorf("process Capture: want ErrNoScreen, got %v", err)
	}
}

func TestTMuxCaptureDeadSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	a := &TMuxAdapter{Process: &ProcessAdapter{}}
	_, err := a.Capture(context.Background(), "daemon-session-99987", 10)
	if !errors.Is(err, ErrTmuxSessionDead) {
		t.Errorf("tmux Capture (dead): want ErrTmuxSessionDead, got %v", err)
	}
}

// TestTMuxCaptureLive — интеграция (реальный tmux; skip если нет).
func TestTMuxCaptureLive(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	a := &TMuxAdapter{Process: &ProcessAdapter{}}
	tmp := t.TempDir()
	ref, err := a.Start(context.Background(), Spec{
		SessionID:  99001,
		WorkingDir: tmp,
		OutputFile: filepath.Join(tmp, "out.log"),
		Command:    "bash",
		Args:       []string{"-c", "echo ALPHA; echo BRAVO; echo CHARLIE; sleep 60"},
	})
	if err != nil {
		t.Fatalf("start tmux session: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background(), ref) })

	// дожидаемся рендера вывода в панели (poll до 4с)
	var lines []string
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		lines, err = a.Capture(context.Background(), ref, 50)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), "CHARLIE") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"ALPHA", "BRAVO", "CHARLIE"} {
		if !strings.Contains(joined, want) {
			t.Errorf("capture missing %q in %q", want, joined)
		}
	}
}
