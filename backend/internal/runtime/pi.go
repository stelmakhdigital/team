package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
}

func (a *PiAdapter) Start(ctx context.Context, spec Spec) (string, error) {
	if spec.ConfigFile != "" && spec.ConfigsDir != "" {
		if err := os.MkdirAll(spec.ConfigsDir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir configs dir: %w", err)
		}
	}
	if spec.Command == "" {
		spec.Command = "pi"
	}
	if spec.Args == nil {
		spec.Args = []string{}
	}
	if spec.ConfigFile != "" {
		spec.Args = append(spec.Args, "--config", spec.ConfigFile)
	}
	spec.Args = append(spec.Args, "--session-id", fmt.Sprint(spec.SessionID))
	return a.TMux.Start(ctx, spec)
}

func (a *PiAdapter) Stop(ctx context.Context, runtimeRef string) error {
	return a.TMux.Stop(ctx, runtimeRef)
}

func (a *PiAdapter) Status(ctx context.Context, runtimeRef string) (*Status, error) {
	return a.TMux.Status(ctx, runtimeRef)
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
