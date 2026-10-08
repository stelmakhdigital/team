package service

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentSpec — парсинг agent.yaml (структура по ТЗ 04_workers / контракту 21 §10).
type AgentSpec struct {
	Name      string        `json:"name" yaml:"name"`
	Version   string        `json:"version" yaml:"version"`
	Runtime   SpecRuntime   `json:"runtime" yaml:"runtime"`
	PiConfig  SpecPiConfig  `json:"pi_config" yaml:"pi_config"`
	Resources SpecResources `json:"resources" yaml:"resources"`
	Startup   SpecStartup   `json:"startup" yaml:"startup"`
	Available bool          `json:"available"` // false — файл не найден/недоступен (stub)
}

type SpecRuntime struct {
	Type    string `json:"type" yaml:"type"`
	Version string `json:"version" yaml:"version"`
}

type SpecPiConfig struct {
	Model       string       `json:"model" yaml:"model"`
	Temperature float64      `json:"temperature" yaml:"temperature"`
	MaxTokens   int          `json:"max_tokens" yaml:"max_tokens"`
	Plugins     []SpecPlugin `json:"plugins" yaml:"plugins"`
	Skills      []SpecSkill  `json:"skills" yaml:"skills"`
	Hooks       []SpecHook   `json:"hooks" yaml:"hooks"`
	MCP         SpecMCP      `json:"mcp" yaml:"mcp"`
}

type SpecPlugin struct {
	Name        string         `json:"name" yaml:"name"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
	Config      map[string]any `json:"config" yaml:"config"`
	Description string         `json:"description,omitempty" yaml:"description"`
}

type SpecSkill struct {
	Path        string `json:"path" yaml:"path"`
	Enabled     bool   `json:"enabled" yaml:"enabled"`
	Description string `json:"description,omitempty" yaml:"description"`
}

type SpecHook struct {
	Path string `json:"path" yaml:"path"`
}

type SpecMCP struct {
	Servers []SpecMCPServer `json:"servers" yaml:"servers"`
}

type SpecMCPServer struct {
	Name    string `json:"name" yaml:"name"`
	URL     string `json:"url" yaml:"url"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
}

type SpecResources struct {
	CPU    string `json:"cpu" yaml:"cpu"`
	Memory string `json:"memory" yaml:"memory"`
	GPU    bool   `json:"gpu" yaml:"gpu"`
}

type SpecStartup struct {
	Files   []SpecStartupFile   `json:"files" yaml:"files"`
	Scripts []SpecStartupScript `json:"scripts" yaml:"scripts"`
}

type SpecStartupFile struct {
	Path         string `json:"path" yaml:"path"`
	Orientation  string `json:"orientation" yaml:"orientation"`
	DeliveryHint string `json:"delivery_hint" yaml:"delivery_hint"`
}

type SpecStartupScript struct {
	Path        string `json:"path" yaml:"path"`
	Description string `json:"description,omitempty" yaml:"description"`
}

// RoleConfigView — GET /roles/{id}/config (контракт 21 §10): role + agent_spec + доступные профили/скиллы/плагины.
type RoleConfigView struct {
	Role              any           `json:"role"`
	AgentSpec         *AgentSpec    `json:"agent_spec"`
	AvailableProfiles []SpecProfile `json:"available_profiles"`
	AvailableSkills   []SpecSkill   `json:"available_skills"`
	AvailablePlugins  []SpecPlugin  `json:"available_plugins"`
}

type SpecProfile struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty"`
}

// agentSpecFile — профили из agent.yaml (pi_config.profiles).
type agentSpecFile struct {
	PiConfig struct {
		Profiles []SpecProfile `yaml:"profiles"`
	} `yaml:"pi_config"`
}

// LoadAgentSpec — читает agent_spec-файл (yaml). Пути ограничены SpecsDir/cwd
// (защита от path traversal). Если файл не найден — stub (Available=false).
func (s *TeamService) LoadAgentSpec(specPath string) (*AgentSpec, []SpecProfile) {
	name := filepath.Base(specPath)
	// stub с пустыми срезами (а не nil): JSON-ответы контрактно содержат []
	stub := &AgentSpec{
		Name: name, Available: false,
		PiConfig: SpecPiConfig{
			Plugins: []SpecPlugin{},
			Skills:  []SpecSkill{},
			Hooks:   []SpecHook{},
			MCP:     SpecMCP{Servers: []SpecMCPServer{}},
		},
		Startup: SpecStartup{Files: []SpecStartupFile{}, Scripts: []SpecStartupScript{}},
	}
	if specPath == "" {
		return &AgentSpec{Name: "", PiConfig: stub.PiConfig, Startup: stub.Startup}, []SpecProfile{}
	}
	// резолв с fallback по расширениям ("pi-lead" → agents/pi-lead.yaml)
	resolved := s.resolveAgentSpecPath(specPath)
	if resolved == "" {
		return stub, []SpecProfile{}
	}
	abs, err := filepath.Abs(resolved)
	if err != nil || !s.pathAllowed(abs) {
		return stub, []SpecProfile{}
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return stub, []SpecProfile{}
	}
	spec := &AgentSpec{}
	if err := yaml.Unmarshal(data, spec); err != nil {
		return stub, []SpecProfile{}
	}
	if spec.Name == "" {
		spec.Name = name
	}
	spec.Available = true
	if spec.PiConfig.Plugins == nil {
		spec.PiConfig.Plugins = []SpecPlugin{}
	}
	if spec.PiConfig.Skills == nil {
		spec.PiConfig.Skills = []SpecSkill{}
	}
	if spec.PiConfig.Hooks == nil {
		spec.PiConfig.Hooks = []SpecHook{}
	}
	if spec.PiConfig.MCP.Servers == nil {
		spec.PiConfig.MCP.Servers = []SpecMCPServer{}
	}
	if spec.Startup.Files == nil {
		spec.Startup.Files = []SpecStartupFile{}
	}
	if spec.Startup.Scripts == nil {
		spec.Startup.Scripts = []SpecStartupScript{}
	}

	var f agentSpecFile
	var profiles []SpecProfile
	if err := yaml.Unmarshal(data, &f); err == nil {
		profiles = f.PiConfig.Profiles
	}
	if profiles == nil {
		profiles = []SpecProfile{}
	}
	return spec, profiles
}

func (s *TeamService) pathAllowed(abs string) bool {
	roots := []string{s.SpecsDir}
	if wd, err := os.Getwd(); err == nil {
		roots = append(roots, wd)
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		if abs == absRoot || strings.HasPrefix(abs, absRoot+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
