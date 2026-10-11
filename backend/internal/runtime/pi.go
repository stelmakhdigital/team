package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PiAdapter — запуск pi-агента (CLI) в tmux-сессии.
//
// Генерирует конфиг сессии (YAML-совместимый JSON) в ConfigsDir/session-<id>.json,
// затем запускает `pi --config <path> --session-id <id>` в tmux (ТЗ 03_adapters).
type PiAdapter struct{ TMux *TMuxAdapter }

func (a *PiAdapter) Name() string { return "pi" }

// AgentSpecFields — данные agent spec, переносимые в конфиг сессии.
type AgentSpecFields struct {
	Name         string   `json:"name"`
	Model        string   `json:"model,omitempty"`
	Thinking     string   `json:"thinking,omitempty"`
	SystemPrompt string   `json:"system_prompt,omitempty"`
	Skills       []string `json:"skills,omitempty"`
	Tools        []string `json:"tools,omitempty"`
	// SandboxLevel — уровень sandbox bash-команд агента (off|dev|untrusted|vm).
	// QA-fix: non-interactive запуск — пропуск trust-диалога через --approve + --sandbox-level.
	SandboxLevel string `json:"sandbox_level,omitempty"`
	// NoExtensions — выключить pi-расширения (--no-extensions) для автономной сессии.
	// QA-fix: интерактивные guard'ы (bash-guard/dir-guard) блокируют non-interactive агента
	// на подтверждении; для автономных daemon-сессий их выключают (governance — через posture).
	NoExtensions bool `json:"no_extensions,omitempty"`
}

func (a *PiAdapter) Start(ctx context.Context, spec Spec) (string, error) {
	if spec.Command == "" {
		spec.Command = "pi"
	}
	if spec.Args == nil {
		spec.Args = []string{}
	}
	// QA-fix: pi CLI не поддерживает `--config`. Строим flags из полей agent spec:
	// --model/--thinking/--tools/--skill, а брифинг/сис-промпт — через --append-system-prompt
	// (сохраняем базовый coding-промпт + инструменты, добавляем контекст сессии).
	if f := spec.Pi; f != nil {
		if f.Model != "" {
			spec.Args = append(spec.Args, "--model", f.Model)
		}
		if f.Thinking != "" {
			spec.Args = append(spec.Args, "--thinking", f.Thinking)
		}
		if len(f.Tools) > 0 {
			spec.Args = append(spec.Args, "--tools", strings.Join(f.Tools, ","))
		}
		for _, sk := range f.Skills {
			spec.Args = append(spec.Args, "--skill", sk)
		}
		if f.NoExtensions {
			spec.Args = append(spec.Args, "--no-extensions")
		}
		if f.SystemPrompt != "" {
			if pf, err := a.writePromptFile(spec, f.SystemPrompt); err == nil && pf != "" {
				spec.Args = append(spec.Args, "--append-system-prompt", pf)
			}
		}
	}
	// QA-fix: non-interactive запуск — auto-trust проекта (пропуск trust-диалога L0/L1).
	spec.Args = append(spec.Args, "--approve")
	noExt := spec.Pi != nil && spec.Pi.NoExtensions
	if !noExt {
		// --sandbox-level — флаг sandbox-расширения; с --no-extensions неизвестен
		// (и без расширений sandbox-диалог не показывается).
		lvl := "off"
		if spec.Pi != nil && spec.Pi.SandboxLevel != "" {
			lvl = spec.Pi.SandboxLevel
		}
		spec.Args = append(spec.Args, "--sandbox-level", lvl)
	}
	spec.Args = append(spec.Args, "--session-id", fmt.Sprint(spec.SessionID))
	// QA-fix: pi — TUI-приложение, требует TTY на stdout → без пайпа в лог-файл.
	spec.DirectTTY = true
	return a.TMux.Start(ctx, spec)
}

// writePromptFile — пишет сис-промпт в текстовый файл (ConfigsDir/session-<id>-prompt.txt)
// и возвращает путь (pi принимает файл в --append-system-prompt; удобно для длинного/многострочного).
func (a *PiAdapter) writePromptFile(spec Spec, prompt string) (string, error) {
	if spec.ConfigsDir == "" {
		return "", nil
	}
	if err := os.MkdirAll(spec.ConfigsDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(spec.ConfigsDir, fmt.Sprintf("session-%d-prompt.txt", spec.SessionID))
	if err := os.WriteFile(path, []byte(prompt), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (a *PiAdapter) Stop(ctx context.Context, runtimeRef string) error {
	return a.TMux.Stop(ctx, runtimeRef)
}

func (a *PiAdapter) Status(ctx context.Context, runtimeRef string) (*Status, error) {
	return a.TMux.Status(ctx, runtimeRef)
}

// Capture — pi работает в tmux → delegate на tmux capture-pane (стл-кадр экрана).
func (a *PiAdapter) Capture(ctx context.Context, runtimeRef string, lines int) ([]string, error) {
	return a.TMux.Capture(ctx, runtimeRef, lines)
}

// Send — pi работает в tmux → delegate на tmux send-keys.
func (a *PiAdapter) Send(ctx context.Context, runtimeRef, text string, submit bool) (SendResult, error) {
	return a.TMux.Send(ctx, runtimeRef, text, submit)
}

// WriteSessionConfig — пишет конфиг сессии для pi (используется session service).
func WriteSessionConfig(path string, f AgentSpecFields) error {
	data := formatConfig(f)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, []byte(data), 0o644)
}

func formatConfig(f AgentSpecFields) string {
	// компактный YAML без внешних зависимостей
	var b string
	b += "name: " + f.Name + "\n"
	if f.Model != "" {
		b += "model: " + f.Model + "\n"
	}
	if f.Thinking != "" {
		b += "thinking: " + f.Thinking + "\n"
	}
	if f.SystemPrompt != "" {
		b += "system_prompt: |-\n"
		for _, line := range splitLines(f.SystemPrompt) {
			b += "  " + line + "\n"
		}
	}
	if len(f.Skills) > 0 {
		b += "skills:\n"
		for _, s := range f.Skills {
			b += "  - " + s + "\n"
		}
	}
	if len(f.Tools) > 0 {
		b += "tools:\n"
		for _, t := range f.Tools {
			b += "  - " + t + "\n"
		}
	}
	return b
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == '\n' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
