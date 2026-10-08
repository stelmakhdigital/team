package service

import (
	"context"
	"fmt"

	"daemon/internal/models"
)

// ---------- Layout helpers (config.layout) ----------

func setLayout(cfg map[string]any, l *Layout) map[string]any {
	if cfg == nil {
		cfg = map[string]any{}
	}
	out := make(map[string]any, len(cfg)+1)
	for k, v := range cfg {
		out[k] = v
	}
	out["layout"] = layoutMap(l)
	return out
}

// layoutMap — сериализуемое представление layout в config (map, а не struct:
// иначе LayoutFromConfig ломается до DB roundtrip — фикс "layout: null после create").
func layoutMap(l *Layout) map[string]any {
	m := map[string]any{"x": l.X, "y": l.Y, "width": l.Width, "height": l.Height}
	if l.Collapsed != nil {
		m["collapsed"] = *l.Collapsed
	}
	if len(l.Path) > 0 {
		pts := make([]any, 0, len(l.Path))
		for _, p := range l.Path {
			pts = append(pts, map[string]any{"x": p.X, "y": p.Y})
		}
		m["path"] = pts
	}
	if l.HasLabel {
		m["label_x"] = l.LabelX
		m["label_y"] = l.LabelY
	}
	return m
}

// LayoutFromConfig — публичный доступ к layout из config (для HTTP-слоя).
func LayoutFromConfig(cfg map[string]any) *Layout { return getLayout(cfg) }

func getLayout(cfg map[string]any) *Layout {
	if cfg == nil {
		return nil
	}
	v, ok := cfg["layout"]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	l := &Layout{}
	if x, ok := m["x"].(float64); ok {
		l.X = x
	}
	if y, ok := m["y"].(float64); ok {
		l.Y = y
	}
	if w, ok := m["width"].(float64); ok {
		l.Width = w
	}
	if h, ok := m["height"].(float64); ok {
		l.Height = h
	}
	if c, ok := m["collapsed"].(bool); ok {
		l.Collapsed = &c
	}
	if p, ok := m["path"].([]any); ok {
		for _, pt := range p {
			pm, ok := pt.(map[string]any)
			if !ok {
				continue
			}
			point := Point{}
			if x, ok := pm["x"].(float64); ok {
				point.X = x
			}
			if y, ok := pm["y"].(float64); ok {
				point.Y = y
			}
			l.Path = append(l.Path, point)
		}
	}
	if lx, ok := m["label_x"].(float64); ok {
		l.LabelX = lx
		l.HasLabel = true
	}
	if ly, ok := m["label_y"].(float64); ok {
		l.LabelY = ly
		l.HasLabel = true
	}
	return l
}

// ---------- ValidateTopology ----------

type ValidateTopologyRequest struct {
	TeamID              int64 `json:"-"`
	CheckCircular       *bool `json:"check_circular,omitempty"`
	CheckOrphans        *bool `json:"check_orphans,omitempty"`
	CheckRequiredFields *bool `json:"check_required_fields,omitempty"`
}

type TopologyCheck struct {
	Code          string    `json:"code"`
	Message       string    `json:"message"`
	Severity      string    `json:"severity"` // "error" | "warning"
	Location      *CheckLoc `json:"location,omitempty"`
	FixSuggestion string    `json:"fix_suggestion,omitempty"`
}

type CheckLoc struct {
	Type string `json:"type"` // "role" | "segment" | "relative"
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ValidateTopologyResponse struct {
	IsValid  bool            `json:"is_valid"`
	Valid    bool            `json:"valid"` // алиас is_valid (контракт frontend)
	Errors   []TopologyCheck `json:"errors"`
	Warnings []TopologyCheck `json:"warnings"`
}

func (s *TeamService) ValidateTopology(ctx context.Context, req ValidateTopologyRequest) (*ValidateTopologyResponse, error) {
	team, err := s.getTeam(ctx, req.TeamID)
	if err != nil {
		return nil, err
	}
	check := func(b *bool) bool { return b == nil || *b }

	detail, err := s.GetTeam(ctx, req.TeamID)
	if err != nil {
		return nil, err
	}

	resp := &ValidateTopologyResponse{IsValid: true, Errors: []TopologyCheck{}, Warnings: []TopologyCheck{}}

	// NOT_EMPTY — базовая проверка: команда обязана содержать топологию.
	if len(detail.Segments) == 0 && len(detail.Roles) == 0 {
		resp.Errors = append(resp.Errors, TopologyCheck{
			Code:          "NOT_EMPTY",
			Message:       "Team has no segments or roles",
			Severity:      "error",
			Location:      &CheckLoc{Type: "team", ID: team.ID, Name: team.Name},
			FixSuggestion: "Add at least one segment with roles",
		})
	}

	if check(req.CheckRequiredFields) {
		for _, r := range detail.Roles {
			if r.AgentSpec == "" {
				resp.Errors = append(resp.Errors, TopologyCheck{
					Code:          "ROLE_NO_AGENT_SPEC",
					Message:       fmt.Sprintf("Role '%s' has no agent_spec", r.Name),
					Severity:      "error",
					Location:      &CheckLoc{Type: "role", ID: r.ID, Name: r.Name},
					FixSuggestion: "Set agent_spec for this role",
				})
			}
		}
	}

	if check(req.CheckCircular) {
		if cycle := findCycle(detail.Relatives); cycle != "" {
			resp.Errors = append(resp.Errors, TopologyCheck{
				Code:          "CIRCULAR_DEPENDENCY",
				Message:       "Circular dependency detected: " + cycle,
				Severity:      "error",
				Location:      &CheckLoc{Type: "team", ID: team.ID, Name: team.Name},
				FixSuggestion: "Remove one of the edges in the cycle",
			})
		}
	}

	if check(req.CheckOrphans) {
		in, out := map[int64]bool{}, map[int64]bool{}
		for _, rw := range detail.Relatives {
			out[rw.FromRoleID] = true
			in[rw.ToRoleID] = true
		}
		for _, r := range detail.Roles {
			if !in[r.ID] && !out[r.ID] {
				resp.Errors = append(resp.Errors, TopologyCheck{
					Code:          "ORPHAN_ROLE",
					Message:       fmt.Sprintf("Role '%s' has no incoming or outgoing edges", r.Name),
					Severity:      "error",
					Location:      &CheckLoc{Type: "role", ID: r.ID, Name: r.Name},
					FixSuggestion: "Connect the role to the team topology",
				})
			} else if !out[r.ID] {
				resp.Warnings = append(resp.Warnings, TopologyCheck{
					Code:     "NO_OUTGOING_EDGES",
					Message:  fmt.Sprintf("Role '%s' has no outgoing edges", r.Name),
					Severity: "warning",
					Location: &CheckLoc{Type: "role", ID: r.ID, Name: r.Name},
				})
			} else if !in[r.ID] {
				resp.Warnings = append(resp.Warnings, TopologyCheck{
					Code:     "NO_INCOMING_EDGES",
					Message:  fmt.Sprintf("Role '%s' has no incoming edges", r.Name),
					Severity: "warning",
					Location: &CheckLoc{Type: "role", ID: r.ID, Name: r.Name},
				})
			}
		}
	}

	resp.IsValid = len(resp.Errors) == 0
	resp.Valid = resp.IsValid
	return resp, nil
}

// findCycle — DFS по направленным рёбрам; возвращает путь цикла ("A → B → A") или "".
func findCycle(rels []*RelativeWithNames) string {
	adj := map[int64][]int64{}
	name := map[int64]string{}
	for _, rw := range rels {
		adj[rw.FromRoleID] = append(adj[rw.FromRoleID], rw.ToRoleID)
		name[rw.FromRoleID] = rw.FromRoleName
		name[rw.ToRoleID] = rw.ToRoleName
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[int64]int{}
	var stack []int64
	var cycle []int64

	var dfs func(u int64) bool
	dfs = func(u int64) bool {
		color[u] = gray
		stack = append(stack, u)
		for _, v := range adj[u] {
			if color[v] == gray {
				// нашли цикл: от v до конца stack + v
				start := 0
				for i, x := range stack {
					if x == v {
						start = i
						break
					}
				}
				cycle = append(append([]int64{}, stack[start:]...), v)
				return true
			}
			if color[v] == white && dfs(v) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		color[u] = black
		return false
	}

	for node := range adj {
		if color[node] == white {
			if dfs(node) {
				parts := make([]string, 0, len(cycle))
				for _, id := range cycle {
					parts = append(parts, name[id])
				}
				return joinArrows(parts)
			}
		}
	}
	return ""
}

func joinArrows(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += " → " + p
	}
	return out
}

var _ models.RelativeType = models.RelDelegatesTo
