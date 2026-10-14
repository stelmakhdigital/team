package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// TMuxAdapter — запуск команды в отдельной tmux-сессии (daemon-session-<id>).
type TMuxAdapter struct{ Process *ProcessAdapter }

func (a *TMuxAdapter) Name() string { return "tmux" }

func (a *TMuxAdapter) sessionName(spec Spec) string {
	return fmt.Sprintf("daemon-session-%d", spec.SessionID)
}

func (a *TMuxAdapter) Start(ctx context.Context, spec Spec) (string, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return "", fmt.Errorf("tmux is not installed")
	}
	name := a.sessionName(spec)
	_ = exec.CommandContext(ctx, "tmux", "kill-session", "-t", name).Run()

	full := spec.Command
	if len(spec.Args) > 0 {
		full += " " + strings.Join(quoteArgs(spec.Args), " ")
	}
	// QA-fix: `--config` убран — pi CLI его не поддерживает; pi-адаптер сам строит
	// корректные флаги (--model/--system-prompt/...) и кладёт их в spec.Args.
	// DirectTTY (TUI-агенты, напр. pi): stdout должен оставаться TTY (иначе TUI
	// не рендерится). Обёртка `script -q -f -e -c CMD LOG` даёт TTY и при этом
	// ЗЕРКАЛИТ вывод в OutputFile — транскрипт-файл, usage-парсинг (context-
	// метрики) и WS session.output (tailer) снова работают для pi-сессий.
	// Служебные строки script («Script started/done…») фильтруются в
	// GetTranscript/тейлере (scriptNoise). Если OutputFile пуст — без зеркала.
	var cmdLine string
	if spec.DirectTTY {
		if spec.OutputFile != "" {
			cmdLine = fmt.Sprintf("script -q -f -e -c %s %s", shq(full), shq(spec.OutputFile))
		} else {
			cmdLine = full
		}
	} else {
		cmdLine = fmt.Sprintf("%s 2>&1 | tee -a %q", full, spec.OutputFile)
	}
	// remain-on-exit задаём ВНУТРИ панельной команды (до старта child) — иначе
	// гонка: child (напр. `exit 3`) может умереть раньше, чем внешний
	// `tmux set-option` после new-session → сессия исчезает целиком → Status
	// даёт stopped/exit 0 вместо failed/exit 3 (флейк crash-детекции).
	cmdLine = fmt.Sprintf("tmux set-option -t %s remain-on-exit on; %s", shq(name), cmdLine)
	if _, err := a.Process.Start(ctx, Spec{
		SessionID:  spec.SessionID,
		WorkingDir: spec.WorkingDir,
		Command:    "tmux",
		Args: []string{"new-session", "-d", "-s", name,
			"-c", orDash(spec.WorkingDir), "bash", "-lc", cmdLine},
		Env: spec.Env,
	}); err != nil {
		return "", err
	}
	// remain-on-exit: после смерти child панель остаётся → Status читает
	// pane_dead_status (exit code) и Reaper различит failed (crash) / stopped.
	_ = exec.CommandContext(ctx, "tmux", "set-option", "-t", name, "remain-on-exit", "on").Run()
	return name, nil
}

func (a *TMuxAdapter) Stop(ctx context.Context, runtimeRef string) error {
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", runtimeRef)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	o := strings.TrimSpace(string(out))
	if strings.Contains(o, "no session") || strings.Contains(o, "can't find session") || strings.Contains(o, "no such session") {
		return nil // уже остановлена/отсутствует — идемпотентно
	}
	return fmt.Errorf("tmux kill-session: %s", o)
}

// Status — живость tmux-сессии. С remain-on-exit (задаётся в Start) мёртвая
// панель остаётся в сессии → exit code читается из pane_dead_status (tmux ≥3.6)
// / pane_exit_code (старые версии). Сессии, убитые целиком (kill-session /
// «can't find session»), трактуются как stopped (exit 0) — как раньше, но
// теперь фразы tmux 3.x тоже распознаются (раньше Reap молча пропускал их и
// crashed-сессии навечно висели «running»).
func (a *TMuxAdapter) Status(ctx context.Context, runtimeRef string) (*Status, error) {
	out, err := exec.CommandContext(ctx, "tmux", "has-session", "-t", runtimeRef).CombinedOutput()
	o := strings.TrimSpace(string(out))
	if err == nil {
		po, _ := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", runtimeRef,
			"#{pane_dead} #{pane_dead_status} #{pane_exit_code}").CombinedOutput()
		f := strings.Fields(string(po))
		if len(f) >= 1 && f[0] == "1" { // панель мертва (child вышел)
			code := 0
			for _, s := range f[1:] {
				if n, perr := strconv.Atoi(s); perr == nil {
					code = n
					break
				}
			}
			st := StateStopped
			if code != 0 {
				st = StateFailed
			}
			return &Status{Alive: false, State: st, ExitCode: intPtr(code)}, nil
		}
		return &Status{Alive: true, State: StateRunning}, nil
	}
	if strings.Contains(o, "no session") || strings.Contains(o, "can't find session") ||
		strings.Contains(o, "no such session") || strings.Contains(o, "no server running") {
		return &Status{Alive: false, State: StateStopped, ExitCode: intPtr(0)}, nil
	}
	return nil, fmt.Errorf("tmux has-session: %s", o)
}

// Capture — стл-кадр панели tmux (последние lines строк) через capture-pane.
// tmux нет → ErrTmuxUnavailable; сессия мертва/отсутствует → ErrTmuxSessionDead.
func (a *TMuxAdapter) Capture(ctx context.Context, runtimeRef string, lines int) ([]string, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return nil, ErrTmuxUnavailable
	}
	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-t", runtimeRef, "-p", "-S", fmt.Sprintf("-%d", lines))
	out, err := cmd.CombinedOutput()
	if err != nil {
		s := strings.TrimSpace(string(out))
		// tmux: «can't find pane/session» (мертвая/отсутствующая сессия) + «no session» (defensive)
		if strings.Contains(s, "can't find") || strings.Contains(s, "no session") {
			return nil, ErrTmuxSessionDead
		}
		return nil, fmt.Errorf("tmux capture-pane: %s", s)
	}
	return splitLines(strings.TrimRight(string(out), "\n")), nil
}

// Send — ввести текст в терминал tmux (literal send-keys) + Enter при submit.
// tmux нет → ErrTmuxUnavailable; сессия мертва/отсутствует → ErrTmuxSessionDead.
func (a *TMuxAdapter) Send(ctx context.Context, runtimeRef, text string, submit bool) (SendResult, error) {
	if _, err := exec.LookPath("tmux"); err != nil {
		return SendResult{}, ErrTmuxUnavailable
	}
	// literal: -l (literal, без интерпретации) + -- (text как аргумент)
	cmd := exec.CommandContext(ctx, "tmux", "send-keys", "-t", runtimeRef, "-l", "--", text)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return SendResult{}, tmuxSendErr(out)
	}
	res := SendResult{Sent: true}
	if submit {
		cmd2 := exec.CommandContext(ctx, "tmux", "send-keys", "-t", runtimeRef, "Enter")
		out2, err2 := cmd2.CombinedOutput()
		if err2 != nil {
			return res, tmuxSendErr(out2)
		}
		res.Submitted = true
	}
	return res, nil
}

// tmuxSendErr — маппинг вывода tmux на sentinel (мертвая сессия) или обычную ошибку.
func tmuxSendErr(out []byte) error {
	s := strings.TrimSpace(string(out))
	if strings.Contains(s, "can't find") || strings.Contains(s, "no session") {
		return ErrTmuxSessionDead
	}
	if strings.Contains(s, "is not installed") {
		return ErrTmuxUnavailable
	}
	return fmt.Errorf("tmux send-keys: %s", s)
}

func orDash(s string) string {
	if s == "" {
		return "."
	}
	return s
}

// shq — shell-кавычка одного аргумента (single-quote + '\”-эскейп) —
// для -c аргумента script-обёртки.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func quoteArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'") {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			out[i] = a
		}
	}
	return out
}
