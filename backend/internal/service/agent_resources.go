package service

import (
	"daemon/internal/runtime"
)

// T5 — Agent resources: role-level override (role.Config["agent_resources"]) поверх
// agent_spec (base). Порядок сохраняется: base-порядок + новые override-записи в конец.

// agentResourcesOverride — parse role.Config["agent_resources"] (structured override):
// {plugins:[…], skills:[…], startup_files:[…]}. Пустые, если override не задан.
func agentResourcesOverride(cfg map[string]any) (plugins, skills, startupFiles []map[string]any) {
	ar, ok := cfg["agent_resources"].(map[string]any)
	if !ok {
		return nil, nil, nil
	}
	return mapList(ar["plugins"]), mapList(ar["skills"]), mapList(ar["startup_files"])
}

func mapList(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// mergeEnabledSkills — base (spec) + override → enabled skill paths (override wins by path).
// Base-порядок сохраняется; новые override-скиллы — в конец.
func mergeEnabledSkills(base []SpecSkill, override []map[string]any) []string {
	merged := make([]SpecSkill, len(base))
	copy(merged, base)
	for _, o := range override {
		path, _ := o["path"].(string)
		if path == "" {
			continue
		}
		found := false
		for i := range merged {
			if merged[i].Path == path {
				if en, ok := o["enabled"].(bool); ok {
					merged[i].Enabled = en
				}
				if desc, ok := o["description"].(string); ok {
					merged[i].Description = desc
				}
				found = true
				break
			}
		}
		if !found {
			s := SpecSkill{Path: path, Enabled: true}
			if en, ok := o["enabled"].(bool); ok {
				s.Enabled = en
			}
			if desc, ok := o["description"].(string); ok {
				s.Description = desc
			}
			merged = append(merged, s)
		}
	}
	var out []string
	for _, s := range merged {
		if s.Enabled {
			out = append(out, s.Path)
		}
	}
	return out
}

// mergeStartupFiles — base (spec) + override → merged startup files (override wins by path).
func mergeStartupFiles(base []SpecStartupFile, override []map[string]any) []runtime.StartupFileField {
	merged := make([]runtime.StartupFileField, len(base))
	for i, f := range base {
		merged[i] = runtime.StartupFileField{Path: f.Path, Orientation: f.Orientation, DeliveryHint: f.DeliveryHint}
	}
	for _, o := range override {
		path, _ := o["path"].(string)
		if path == "" {
			continue
		}
		found := false
		for i := range merged {
			if merged[i].Path == path {
				if ori, ok := o["orientation"].(string); ok {
					merged[i].Orientation = ori
				}
				if hint, ok := o["delivery_hint"].(string); ok {
					merged[i].DeliveryHint = hint
				}
				found = true
				break
			}
		}
		if !found {
			f := runtime.StartupFileField{Path: path}
			if ori, ok := o["orientation"].(string); ok {
				f.Orientation = ori
			}
			if hint, ok := o["delivery_hint"].(string); ok {
				f.DeliveryHint = hint
			}
			merged = append(merged, f)
		}
	}
	return merged
}

// mergePlugins — base (spec) + override → merged plugins (override wins by name).
// Возвращает список map[string]any (для persist в конфиг сессии).
func mergePlugins(base []SpecPlugin, override []map[string]any) []map[string]any {
	merged := make([]map[string]any, 0, len(base)+len(override))
	for _, p := range base {
		merged = append(merged, map[string]any{
			"name": p.Name, "enabled": p.Enabled, "config": p.Config, "description": p.Description,
		})
	}
	for _, o := range override {
		name, _ := o["name"].(string)
		if name == "" {
			continue
		}
		found := false
		for i := range merged {
			if merged[i]["name"] == name {
				for k, v := range o {
					merged[i][k] = v
				}
				found = true
				break
			}
		}
		if !found {
			m := map[string]any{}
			for k, v := range o {
				m[k] = v
			}
			merged = append(merged, m)
		}
	}
	return merged
}
