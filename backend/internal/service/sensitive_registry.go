package service

import "regexp"

// ---------- P3.16: Sensitive-registry (чувствительные операции) ----------
//
// Детерминированный кодовый реестр правил: {method, pathPattern, operation,
// reversible}. matchSensitive(method, path) → (operation, reversible, ok).
// Порядок важен: специфичные правила раньше обобщённых (DELETE any — в конце).
//
// «Работа вне project-path» — отдельная эвристика, по умолчанию НЕ в v1
// (follow-up, задокументировано в api-decisions §P3.16).

type sensitiveRule struct {
	method     string
	pattern    *regexp.Regexp
	operation  string
	reversible bool
}

var sensitiveRules = []sensitiveRule{
	{method: "PUT", pattern: regexp.MustCompile(`^/api/v1/config/[^/]+$`), operation: "config.set", reversible: true},
	{method: "POST", pattern: regexp.MustCompile(`^/api/v1/workflow-instances/[^/]+/abort$`), operation: "workflow.abort", reversible: false},
	{method: "DELETE", pattern: regexp.MustCompile(`^/api/v1/sessions/[^/]+$`), operation: "session.stop", reversible: false},
	{method: "POST", pattern: regexp.MustCompile(`^/api/v1/sessions/[^/]+/handover$`), operation: "session.handover", reversible: true},
	{method: "DELETE", pattern: regexp.MustCompile(`^/api/v1/library/[^/]+$`), operation: "library.prune", reversible: false},
	// обобщённый: любой DELETE (stop/прune/archive/relatives — все необратимые)
	{method: "DELETE", pattern: regexp.MustCompile(`^/`), operation: "delete", reversible: false},
}

// MatchSensitive — sensitive ли операция (method, path) + её классификация.
// ok=false → не чувствительная операция (не паркуем).
func MatchSensitive(method, path string) (operation string, reversible bool, ok bool) {
	for _, r := range sensitiveRules {
		if r.method != method {
			continue
		}
		if r.pattern.MatchString(path) {
			return r.operation, r.reversible, true
		}
	}
	return "", false, false
}
