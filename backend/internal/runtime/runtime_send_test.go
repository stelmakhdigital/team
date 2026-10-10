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

// ---------- P2.11: Send (runtime-адаптеры) ----------

func TestProcessSendNoScreen(t *testing.T) {
	a := &ProcessAdapter{}
	_, err := a.Send(context.Background(), "ref", "hello", true)
	if !errors.Is(err, ErrNoScreen) {
		t.Errorf("process Send: want ErrNoScreen, got %v", err)
	}
}

func TestTMuxSendDeadSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	a := &TMuxAdapter{Process: &ProcessAdapter{}}
	_, err := a.Send(context.Background(), "daemon-session-99988", "hello", true)
	if !errors.Is(err, ErrTmuxSessionDead) {
		t.Errorf("tmux Send (dead): want ErrTmuxSessionDead, got %v", err)
	}
}

// TestTMuxSendLive — интеграция (реальный tmux; skip если нет). `cat` эхоит ввод.
func TestTMuxSendLive(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	a := &TMuxAdapter{Process: &ProcessAdapter{}}
	tmp := t.TempDir()
	ref, err := a.Start(context.Background(), Spec{
		SessionID:  99002,
		WorkingDir: tmp,
		OutputFile: filepath.Join(tmp, "out.log"),
		Command:    "cat",
	})
	if err != nil {
		t.Fatalf("start tmux session: %v", err)
	}
	t.Cleanup(func() { _ = a.Stop(context.Background(), ref) })
	// дождаться готовности панели
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := a.Capture(context.Background(), ref, 5); err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Send: literal text + Enter
	marker := "SENT-MARKER-XYZ"
	res, err := a.Send(context.Background(), ref, marker, true)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !res.Sent || !res.Submitted {
		t.Errorf("Send result = %+v, want Sent+Submitted", res)
	}
	// cat эхоит → marker в панели
	var lines []string
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		lines, err = a.Capture(context.Background(), ref, 50)
		if err == nil && strings.Contains(strings.Join(lines, "\n"), marker) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("capture after send: %v", err)
	}
	if !strings.Contains(strings.Join(lines, "\n"), marker) {
		t.Errorf("sent text not echoed in pane: %q", strings.Join(lines, "\n"))
	}
}
