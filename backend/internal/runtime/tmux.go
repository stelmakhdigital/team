package runtime

import (
	"context"
	"fmt"
	"os/exec"
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
	if spec.ConfigFile != "" {
		full += " --config " + quoteArgs([]string{spec.ConfigFile})[0]
	}
	// detached; stdout/stderr — в transcript-файл
	cmdLine := fmt.Sprintf("%s 2>&1 | tee -a %q", full, spec.OutputFile)
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
	return name, nil
}

func (a *TMuxAdapter) Stop(ctx context.Context, runtimeRef string) error {
	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", runtimeRef)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if strings.Contains(string(out), "no session") {
		return nil // уже остановлена — идемпотентно
	}
	return fmt.Errorf("tmux kill-session: %s", strings.TrimSpace(string(out)))
}

func (a *TMuxAdapter) Status(ctx context.Context, runtimeRef string) (*Status, error) {
	out, err := exec.CommandContext(ctx, "tmux", "has-session", "-t", runtimeRef).CombinedOutput()
	if err == nil {
		return &Status{Alive: true, State: StateRunning}, nil
	}
	if strings.Contains(string(out), "no session") {
		return &Status{Alive: false, State: StateStopped, ExitCode: intPtr(0)}, nil
	}
	return nil, fmt.Errorf("tmux has-session: %s", strings.TrimSpace(string(out)))
}

func orDash(s string) string {
	if s == "" {
		return "."
	}
	return s
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
