package runtime

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Status-жизненный цикл tmux-сессии (реальный tmux; skip если нет):
//  1. crashed child (exit 3, remain-on-exit из Start) → Status: failed + exit_code 3
//  2. живая сессия → alive/running
//  3. kill-session → stopped + exit 0 (фраза tmux ≥3.x «can't find session»)
//  4. DirectTTY + script-зеркало → OutputFile получает вывод child
func TestTMuxStatusLifecycle(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}
	if _, err := exec.LookPath("script"); err != nil {
		t.Skip("script (util-linux) not available")
	}
	a := &TMuxAdapter{Process: &ProcessAdapter{}}
	ctx := context.Background()
	tmp := t.TempDir()
	waitFor := func(pred func() bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if pred() {
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
		t.Fatal("condition not met in 10s")
	}

	// 1) crashed → failed + exit 3
	name, err := a.Start(ctx, Spec{
		SessionID: 991001, Command: "/bin/sh", Args: []string{"-c", "exit 3"},
		OutputFile: filepath.Join(tmp, "s1.log"), DirectTTY: true,
	})
	if err != nil {
		t.Fatalf("start(crash): %v", err)
	}
	waitFor(func() bool {
		st, err := a.Status(ctx, name)
		return err == nil && !st.Alive
	})
	st, err := a.Status(ctx, name)
	if err != nil {
		t.Fatalf("status(crash): %v", err)
	}
	if st.State != StateFailed {
		t.Errorf("crashed: want state=failed, got %s", st.State)
	}
	if st.ExitCode == nil || *st.ExitCode != 3 {
		t.Errorf("crashed: want exit_code 3, got %v", st.ExitCode)
	}
	_ = a.Stop(ctx, name) // зомби-сессия (remain-on-exit) — чистим

	// 2) живая → alive
	name2, err := a.Start(ctx, Spec{
		SessionID: 991002, Command: "bash", Args: []string{"-lc", "sleep 30"},
		OutputFile: filepath.Join(tmp, "s2.log"),
	})
	if err != nil {
		t.Fatalf("start(live): %v", err)
	}
	if st, err := a.Status(ctx, name2); err != nil || !st.Alive {
		t.Errorf("live: want alive, got alive=%v err=%v", st, err)
	}
	// 3) kill-session → stopped + exit 0
	if err := a.Stop(ctx, name2); err != nil {
		t.Fatalf("stop(live): %v", err)
	}
	st, err = a.Status(ctx, name2)
	if err != nil {
		t.Fatalf("status(after kill): %v", err)
	}
	if st.Alive || st.State != StateStopped {
		t.Errorf("killed: want stopped/alive=false, got %s alive=%v", st.State, st.Alive)
	}
	// фраза tmux 3.x на несуществующей сессии — не ошибка, а «мертва»
	if st, err := a.Status(ctx, "daemon-session-9999999"); err != nil || st.Alive {
		t.Errorf("nonexistent: want dead w/o error, got alive=%v err=%v", st, err)
	}

	// 4) DirectTTY + script → stdout зеркалится в OutputFile (transcript/usage/tail)
	log3 := filepath.Join(tmp, "s3.log")
	name3, err := a.Start(ctx, Spec{
		SessionID: 991003, Command: "printf", Args: []string{"mirror-ok"},
		OutputFile: log3, DirectTTY: true,
	})
	if err != nil {
		t.Fatalf("start(mirror): %v", err)
	}
	waitFor(func() bool {
		data, err := os.ReadFile(log3)
		return err == nil && strings.Contains(string(data), "mirror-ok")
	})
	_ = a.Stop(ctx, name3)
}
