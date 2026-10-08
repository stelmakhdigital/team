package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// MetricsService — dashboard/metrics (контракт 20 §3.5): time series по БД.
// llm_tokens = 0 (LLM-агентов в сборке нет; появится с runtime-агентами).
type MetricsService struct {
	db *sql.DB
}

func NewMetricsService(db *sql.DB) *MetricsService { return &MetricsService{db: db} }

// MetricsRange — 1h | 24h | 7d.
type MetricsRange string

const (
	MetricsRange1H  MetricsRange = "1h"
	MetricsRange24H MetricsRange = "24h"
	MetricsRange7D  MetricsRange = "7d"
)

const metricsPoints = 12

type TimeSeriesPoint struct {
	Timestamp string  `json:"timestamp"`
	Value     float64 `json:"value"`
}

// MetricsResult — GET /api/v1/dashboard/metrics (контракт 20 §3.5).
type MetricsResult struct {
	TimeRange struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"time_range"`
	Metrics struct {
		TasksCreated   []TimeSeriesPoint `json:"tasks_created"`
		TasksCompleted []TimeSeriesPoint `json:"tasks_completed"`
		SessionsActive []TimeSeriesPoint `json:"sessions_active"`
		QueueSize      []TimeSeriesPoint `json:"queue_size"`
		LLMTokens      []TimeSeriesPoint `json:"llm_tokens"`
	} `json:"metrics"`
}

func (r MetricsRange) duration() (time.Duration, error) {
	switch r {
	case MetricsRange1H:
		return time.Hour, nil
	case MetricsRange24H:
		return 24 * time.Hour, nil
	case MetricsRange7D:
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid range %q (use 1h, 24h or 7d)", r)
	}
}

func (s *MetricsService) Get(ctx context.Context, r MetricsRange) (*MetricsResult, error) {
	dur, err := r.duration()
	if err != nil {
		return nil, NewValidation(err.Error())
	}
	end := time.Now().UTC().Truncate(dur / metricsPoints)
	start := end.Add(-dur)
	bucket := dur / metricsPoints

	now := end.Add(bucket) // события до "текущей минуты" включительно
	created, err := s.countsByCreatedAt(ctx,
		`SELECT created_at FROM queue_tasks WHERE created_at >= ? AND created_at < ?`, start, now)
	if err != nil {
		return nil, err
	}
	completed, err := s.countsByCreatedAt(ctx,
		`SELECT created_at FROM history_status WHERE to_state = 'done' AND created_at >= ? AND created_at < ?`, start, now)
	if err != nil {
		return nil, err
	}

	out := &MetricsResult{}
	out.TimeRange.Start = start.Format(time.RFC3339)
	out.TimeRange.End = end.Format(time.RFC3339)
	createdBuckets := bucketize(start, bucket, created)
	completedBuckets := bucketize(start, bucket, completed)
	// события в "хвосте" (после end, до now) вписываем в последний бакет
	// (UI: графики "до текущей минуты")
	overflow := 0
	bucketSec := int64(bucket.Seconds())
	for sec := range created {
		if sec >= end.Unix() {
			overflow += 1
		}
	}
	if overflow > 0 && len(createdBuckets) > 0 {
		_ = bucketSec
		last := &createdBuckets[len(createdBuckets)-1]
		last.Value += float64(overflow)
	}
	overflow = 0
	for sec := range completed {
		if sec >= end.Unix() {
			overflow += 1
		}
	}
	if overflow > 0 && len(completedBuckets) > 0 {
		last := &completedBuckets[len(completedBuckets)-1]
		last.Value += float64(overflow)
	}
	out.Metrics.TasksCreated = createdBuckets
	out.Metrics.TasksCompleted = completedBuckets
	out.Metrics.SessionsActive, err = s.sessionsActiveSeries(ctx, start, end, bucket)
	if err != nil {
		return nil, err
	}
	out.Metrics.QueueSize, err = s.queueSizeSeries(ctx, start, end, bucket)
	if err != nil {
		return nil, err
	}
	out.Metrics.LLMTokens = make([]TimeSeriesPoint, 0, metricsPoints)
	for i := 0; i < metricsPoints; i++ {
		t := start.Add(time.Duration(i) * bucket)
		out.Metrics.LLMTokens = append(out.Metrics.LLMTokens,
			TimeSeriesPoint{Timestamp: t.Format(time.RFC3339), Value: 0})
	}
	return out, nil
}

// countsByCreatedAt — timestamp'ы → unix-секунды (1 на строку).
func (s *MetricsService) countsByCreatedAt(ctx context.Context, query string, start, end time.Time) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, query, start.Format(time.RFC3339Nano), end.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]int{}
	for rows.Next() {
		var ts string
		if err := rows.Scan(&ts); err != nil {
			return nil, err
		}
		t, err := parseRFC3339(ts)
		if err != nil {
			continue
		}
		out[t.Unix()]++
	}
	return out, rows.Err()
}

func parseRFC3339(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}

// bucketize — counts по unix-секундам → N точек (начало каждого бакета).
func bucketize(start time.Time, bucket time.Duration, counts map[int64]int) []TimeSeriesPoint {
	out := make([]TimeSeriesPoint, 0, metricsPoints)
	bucketSec := int64(bucket.Seconds())
	for i := 0; i < metricsPoints; i++ {
		t := start.Add(time.Duration(i) * bucket)
		bucketStart := t.Unix() / bucketSec * bucketSec
		sum := 0
		for sec, n := range counts {
			if sec/bucketSec*bucketSec == bucketStart {
				sum += n
			}
		}
		out = append(out, TimeSeriesPoint{Timestamp: t.Format(time.RFC3339), Value: float64(sum)})
	}
	return out
}

// sessionsActiveSeries — snapshot на конец каждого бакета:
// запущенные и ещё не остановленные сессии.
func (s *MetricsService) sessionsActiveSeries(ctx context.Context, start, end time.Time, bucket time.Duration) ([]TimeSeriesPoint, error) {
	out := make([]TimeSeriesPoint, 0, metricsPoints)
	for i := 0; i < metricsPoints; i++ {
		t := start.Add(time.Duration(i+1) * bucket)
		var n int
		err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM sessions
			WHERE started_at IS NOT NULL AND started_at <= ?
			  AND (stopped_at IS NULL OR stopped_at > ?)`,
			t.Format(time.RFC3339Nano), t.Format(time.RFC3339Nano)).Scan(&n)
		if err != nil {
			return nil, err
		}
		out = append(out, TimeSeriesPoint{Timestamp: t.Format(time.RFC3339), Value: float64(n)})
	}
	return out, nil
}

// queueSizeSeries — snapshot на конец каждого бакета: незакрытые задачи
// (нет history to_state done|canceled до t).
func (s *MetricsService) queueSizeSeries(ctx context.Context, start, end time.Time, bucket time.Duration) ([]TimeSeriesPoint, error) {
	out := make([]TimeSeriesPoint, 0, metricsPoints)
	for i := 0; i < metricsPoints; i++ {
		t := start.Add(time.Duration(i+1) * bucket)
		ts := t.Format(time.RFC3339Nano)
		var n int
		err := s.db.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM queue_tasks t
			WHERE t.created_at <= ?
			  AND NOT EXISTS (
				SELECT 1 FROM history_status h
				WHERE h.queue_task_id = t.id
				  AND h.to_state IN ('done', 'canceled')
				  AND h.created_at <= ?)`, ts, ts).Scan(&n)
		if err != nil {
			return nil, err
		}
		out = append(out, TimeSeriesPoint{Timestamp: t.Format(time.RFC3339), Value: float64(n)})
	}
	return out, nil
}
