package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// P2.13 — Starter bundles: готовый набор, который apply'ится в один клик.
// Детерминированный + идемпотентный seed (ON CONFLICT (type,name) DO NOTHING).
// Вызывается при старте демона (после миграций); повторный вызов не дублирует.

const starterSeedTime = "2026-01-01T00:00:00Z"

type starterDef struct {
	typ  string
	name string
	desc string
	spec map[string]any
}

// starterSpecs — стартеры (2 шт., type=team + type=workflow).
func starterSpecs() []starterDef {
	devTeam := map[string]any{
		"name":        "dev-team",
		"description": "Команда разработки: планирование → реализация → ревью",
		"segments": []any{
			map[string]any{"name": "core", "description": "Ядро команды"},
		},
		"roles": []any{
			map[string]any{"segment": "core", "name": "lead", "agent_spec": "agents/pi-lead.yaml"},
			map[string]any{"segment": "core", "name": "worker", "agent_spec": "agents/pi-worker.yaml"},
			map[string]any{"segment": "core", "name": "reviewer", "agent_spec": "agents/pi-reviewer.yaml"},
		},
		"relatives": []any{
			map[string]any{"from": "core.lead", "to": "core.worker", "type": "delegates_to"},
			map[string]any{"from": "core.worker", "to": "core.reviewer", "type": "delegates_to"},
		},
	}
	reviewWorkflow := map[string]any{
		"name":        "review-workflow",
		"description": "Воркфлоу ревью: реализация → ревью → завершение",
		"blocks": []any{
			map[string]any{"type": "agent", "label": "implement",
				"config":   map[string]any{"role_name": "worker", "prompt": "Реализуй задачу"},
				"position": map[string]any{"x": 0, "y": 0}},
			map[string]any{"type": "agent", "label": "review",
				"config":   map[string]any{"role_name": "reviewer", "prompt": "Проревью изменения"},
				"position": map[string]any{"x": 200, "y": 0}},
			map[string]any{"type": "task", "label": "done",
				"config":   map[string]any{},
				"position": map[string]any{"x": 400, "y": 0}},
		},
		"connections": []any{
			map[string]any{"from_block_id": 0, "to_block_id": 1},
			map[string]any{"from_block_id": 1, "to_block_id": 2},
		},
	}
	return []starterDef{
		{typ: "team", name: "dev-team", desc: "Команда разработки: планирование → реализация → ревью", spec: devTeam},
		{typ: "workflow", name: "review-workflow", desc: "Воркфлоу ревью: реализация → ревью → завершение", spec: reviewWorkflow},
	}
}

// SeedStarters — вставляет стартеры (is_starter=1, group_name=starters,
// version=1.0.0, author=team, is_public=1). Идемпотентно: ON CONFLICT (type,name)
// DO NOTHING (рестарт/повтор не дублирует).
func SeedStarters(ctx context.Context, db *sql.DB) error {
	for _, s := range starterSpecs() {
		spec, err := json.Marshal(s.spec)
		if err != nil {
			return fmt.Errorf("marshal starter %q spec: %w", s.name, err)
		}
		if _, err := db.ExecContext(ctx, `
			INSERT INTO library_items (type, name, description, group_name, version, author,
				is_public, tags, is_starter, spec, created_at, updated_at)
			VALUES (?, ?, ?, 'starters', '1.0.0', 'team', 1, '["starter"]', 1, ?, ?, ?)
			ON CONFLICT (type, name) DO NOTHING`,
			s.typ, s.name, s.desc, string(spec), starterSeedTime, starterSeedTime); err != nil {
			return fmt.Errorf("seed starter %q: %w", s.name, err)
		}
	}
	return nil
}
