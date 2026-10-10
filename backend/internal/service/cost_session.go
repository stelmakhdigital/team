package service

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"daemon/internal/models"
	"daemon/internal/repository"
)

// P2.14 — Cost tracking: агрегация стоимости per task / per project.
// Токены берутся из того же log-tail, что и live-метрики (session_live.go).
// Bounded: не более costAggSessionLimit сессий на агрегат (документировано).

const costAggSessionLimit = 100

// costSeriesSessionLimit — bounded: сколько сессий читаем для dashboard-серии
// стоимости (log reads; 50 × ~64KB tail). Документировано: больше не берём.
const costSeriesSessionLimit = 50

// sessionCostInput — (in, out, model) для расчёта стоимости одной сессии.
// in/out nil = нет usage (не парсится). Читает тот же log-tail, что и enrichLiveMetrics.
func (s *SessionService) sessionCostInput(sess *models.Session) (in, out *int64, model string) {
	model = s.sessionModel(sess)
	path := s.sessionLogPath(sess.ID)
	if path == "" {
		return nil, nil, model
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, model
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, model
	}
	off := int64(0)
	if fi.Size() > usageScanTailBytes {
		off = fi.Size() - usageScanTailBytes
	}
	buf := make([]byte, fi.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil {
		return nil, nil, model
	}
	totalIn, totalOut, found := 0, 0, false
	for _, line := range strings.Split(string(buf), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		usage := extractUsage(rec)
		if usage == nil {
			continue
		}
		totalIn += intNum(usage["input_tokens"]) + intNum(usage["cache_read_input_tokens"])
		totalOut += intNum(usage["output_tokens"])
		found = true
	}
	if !found {
		return nil, nil, model
	}
	i, o := int64(totalIn), int64(totalOut)
	return &i, &o, model
}

// costForSessions — сумма cost по сессиям (bounded список уже передан).
func (s *SessionService) costForSessions(ctx context.Context, sessions []*models.Session) *CostInfo {
	if s.cost == nil || len(sessions) == 0 {
		return NoDataCost()
	}
	items := make([]*CostInfo, 0, len(sessions))
	for _, sess := range sessions {
		in, out, model := s.sessionCostInput(sess)
		items = append(items, s.cost.SessionCost(ctx, in, out, model))
	}
	return s.cost.SumCost(items)
}

// CostForTask — оценка стоимости задачи: сумма cost её сессий (bounded).
func (s *SessionService) CostForTask(ctx context.Context, taskID int64) *CostInfo {
	if s.cost == nil {
		return NoDataCost()
	}
	sessions, _, err := s.sessions.List(ctx, s.db,
		repository.SessionFilter{QueueTaskID: &taskID, Limit: costAggSessionLimit})
	if err != nil {
		return NoDataCost()
	}
	return s.costForSessions(ctx, sessions)
}

// CostForProject — оценка стоимости проекта: сумма cost сессий проекта (bounded).
func (s *SessionService) CostForProject(ctx context.Context, projectID int64) *CostInfo {
	if s.cost == nil {
		return NoDataCost()
	}
	sessions, _, err := s.sessions.List(ctx, s.db,
		repository.SessionFilter{ProjectID: &projectID, Limit: costAggSessionLimit})
	if err != nil {
		return NoDataCost()
	}
	return s.costForSessions(ctx, sessions)
}

// CostForAll — оценка общей стоимости (все сессии, bounded). Для budget-порога.
func (s *SessionService) CostForAll(ctx context.Context) *CostInfo {
	if s.cost == nil {
		return NoDataCost()
	}
	sessions, _, err := s.sessions.List(ctx, s.db, repository.SessionFilter{Limit: costSeriesSessionLimit})
	if err != nil {
		return NoDataCost()
	}
	return s.costForSessions(ctx, sessions)
}

// CostSeries — стоимость по сессиям, bucketed по периоду (для dashboard
// llm_cost_usd). Bounded: не более costSeriesSessionLimit сессий (последние по
// updated_at); фильтр started_at в [start, end+bucket) в Go. Сессии без started_at
// и без данных → 0 в бакете. 12 точек (metricsPoints), как прочие серии.
func (s *SessionService) CostSeries(ctx context.Context, start, end time.Time, bucket time.Duration) ([]TimeSeriesPoint, error) {
	out := make([]TimeSeriesPoint, metricsPoints)
	for i := 0; i < metricsPoints; i++ {
		out[i] = TimeSeriesPoint{Timestamp: start.Add(time.Duration(i) * bucket).Format(time.RFC3339), Value: 0}
	}
	if s.cost == nil {
		return out, nil
	}
	sessions, _, err := s.sessions.List(ctx, s.db, repository.SessionFilter{Limit: costSeriesSessionLimit})
	if err != nil {
		return out, err
	}
	now := end.Add(bucket) // события до "текущей минуты" включительно
	for _, sess := range sessions {
		if sess.StartedAt == nil || sess.StartedAt.Before(start) || !sess.StartedAt.Before(now) {
			continue
		}
		in, outTok, model := s.sessionCostInput(sess)
		ci := s.cost.SessionCost(ctx, in, outTok, model)
		if !ci.HasData || ci.CostUSD == nil {
			continue
		}
		idx := int(sess.StartedAt.Sub(start) / bucket)
		if idx < 0 {
			idx = 0
		}
		if idx >= len(out) {
			idx = len(out) - 1
		}
		out[idx].Value += *ci.CostUSD
	}
	return out, nil
}
