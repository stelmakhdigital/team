# Мониторинг и метрики — Prometheus, Grafana для сессий

## Архитектура мониторинга

```
┌──────────────────────────────────────────────────────────┐
│                    MONITORING STACK                      │
├──────────────────────────────────────────────────────────┤
│                                                          │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Daemon (Go)                                       │ │
│  │                                                    │ │
│  │  ┌──────────────────────────────────────────────┐ │ │
│  │  │  Prometheus Metrics                          │ │ │
│  │  │  - Counters (tasks_created, sessions_started)│ │ │
│  │  │  - Gauges (active_sessions, queue_size)     │ │ │
│  │  │  - Histograms (task_duration, latency)      │ │ │
│  │  └──────────────────────────────────────────────┘ │ │
│  │         │                                         │ │
│  │         │ /metrics (port 9090)                   │ │
│  └─────────┼─────────────────────────────────────────┘ │
│            │                                           │
│            ▼                                           │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Prometheus Server                                 │ │
│  │                                                    │ │
│  │  - Scrapes /metrics каждые 15s                   │ │
│  │  - Stores time-series data                        │ │
│  │  - PromQL queries                                 │ │
│  └────────────────────────────────────────────────────┘ │
│            │                                           │
│            │ Query API                                 │
│            ▼                                           │
│  ┌────────────────────────────────────────────────────┐ │
│  │  Grafana                                           │ │
│  │                                                    │ │
│  │  - Dashboards                                     │ │
│  │  - Alerts                                         │ │
│  │  - Visualizations                                 │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
└──────────────────────────────────────────────────────────┘
```


______________________________________________________________________

## 1. Метрики в Daemon

### 1.1 Prometheus Client в Go

**Инициализация метрик:**

```go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
    // Sessions
    sessionsTotal      prometheus.Counter
    sessionsActive     prometheus.Gauge
    sessionsByType     *prometheus.GaugeVec
    sessionsDuration   prometheus.Histogram
    
    // Tasks
    tasksTotal         prometheus.Counter
    tasksByState       *prometheus.GaugeVec
    tasksDuration      prometheus.Histogram
    tasksByRole        *prometheus.CounterVec
    
    // Queue
    queueSize          prometheus.Gauge
    queueByState       *prometheus.GaugeVec
    
    // Errors
    errorsTotal        *prometheus.CounterVec
    
    // Config reloads
    configReloadsTotal prometheus.Counter
    configReloadsFailed prometheus.Counter
    
    // LLM
    llmRequestsTotal   *prometheus.CounterVec
    llmRequestDuration prometheus.Histogram
    llmTokensTotal     *prometheus.CounterVec
}

func NewMetrics() *Metrics {
    return &Metrics{
        // Sessions
        sessionsTotal: promauto.NewCounter(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "sessions_total",
            Help:      "Total number of sessions created",
        }),
        
        sessionsActive: promauto.NewGauge(prometheus.GaugeOpts{
            Namespace: "daemon",
            Name:      "sessions_active",
            Help:      "Number of currently active sessions",
        }),
        
        sessionsByType: promauto.NewGaugeVec(prometheus.GaugeOpts{
            Namespace: "daemon",
            Name:      "sessions_by_type",
            Help:      "Number of sessions by runtime type",
        }, []string{"runtime_type"}),
        
        sessionsDuration: promauto.NewHistogram(prometheus.HistogramOpts{
            Namespace: "daemon",
            Name:      "session_duration_seconds",
            Help:      "Duration of sessions in seconds",
            Buckets:   prometheus.DefBuckets,
        }),
        
        // Tasks
        tasksTotal: promauto.NewCounter(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "tasks_total",
            Help:      "Total number of tasks created",
        }),
        
        tasksByState: promauto.NewGaugeVec(prometheus.GaugeOpts{
            Namespace: "daemon",
            Name:      "tasks_by_state",
            Help:      "Number of tasks by state",
        }, []string{"state"}),
        
        tasksDuration: promauto.NewHistogram(prometheus.HistogramOpts{
            Namespace: "daemon",
            Name:      "task_duration_seconds",
            Help:      "Duration of tasks in seconds",
            Buckets:   []float64{1, 5, 10, 30, 60, 300, 600, 1800, 3600},
        }),
        
        tasksByRole: promauto.NewCounterVec(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "tasks_by_role",
            Help:      "Number of tasks by role",
        }, []string{"role"}),
        
        // Queue
        queueSize: promauto.NewGauge(prometheus.GaugeOpts{
            Namespace: "daemon",
            Name:      "queue_size",
            Help:      "Current size of task queue",
        }),
        
        queueByState: promauto.NewGaugeVec(prometheus.GaugeOpts{
            Namespace: "daemon",
            Name:      "queue_by_state",
            Help:      "Number of queue items by state",
        }, []string{"state"}),
        
        // Errors
        errorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "errors_total",
            Help:      "Total number of errors",
        }, []string{"type", "component"}),
        
        // Config reloads
        configReloadsTotal: promauto.NewCounter(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "config_reloads_total",
            Help:      "Total number of config reloads",
        }),
        
        configReloadsFailed: promauto.NewCounter(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "config_reloads_failed_total",
            Help:      "Total number of failed config reloads",
        }),
        
        // LLM
        llmRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "llm_requests_total",
            Help:      "Total number of LLM requests",
        }, []string{"model", "status"}),
        
        llmRequestDuration: promauto.NewHistogram(prometheus.HistogramOpts{
            Namespace: "daemon",
            Name:      "llm_request_duration_seconds",
            Help:      "Duration of LLM requests",
            Buckets:   []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
        }),
        
        llmTokensTotal: promauto.NewCounterVec(prometheus.CounterOpts{
            Namespace: "daemon",
            Name:      "llm_tokens_total",
            Help:      "Total number of tokens used",
        }, []string{"model", "type"}),  // type: prompt, completion
    }
}
```


### 1.2 HTTP Handler для /metrics

```go
package api

import (
    "github.com/prometheus/client_golang/prometheus/promhttp"
    "net/http"
)

func NewMetricsHandler() http.Handler {
    return promhttp.Handler()
}

// В main.go
func main() {
    // ... инициализация
    
    metrics := metrics.NewMetrics()
    
    // Запустить HTTP сервер для метрик
    go func() {
        mux := http.NewServeMux()
        mux.Handle("/metrics", NewMetricsHandler())
        
        log.Info("starting metrics server", "port", 9090)
        if err := http.ListenAndServe(":9090", mux); err != nil {
            log.Error("metrics server failed", "err", err)
        }
    }()
    
    // ... остальной код
}
```


______________________________________________________________________

### 1.3 Инструментирование кода

**Session Manager:**

```go
package session

func (m *SessionManager) StartSession(ctx context.Context, req StartSessionRequest) (*Session, error) {
    start := time.Now()
    
    session, err := m.startSessionInternal(ctx, req)
    
    duration := time.Since(start).Seconds()
    
    // Обновить метрики
    if err == nil {
        m.metrics.sessionsTotal.Inc()
        m.metrics.sessionsActive.Inc()
        m.metrics.sessionsByType.WithLabelValues(string(req.RuntimeType)).Inc()
        m.metrics.sessionsDuration.Observe(duration)
    } else {
        m.metrics.errorsTotal.WithLabelValues("session_start", "session_manager").Inc()
    }
    
    return session, err
}

func (m *SessionManager) StopSession(ctx context.Context, sessionID int64, reason string) error {
    err := m.stopSessionInternal(ctx, sessionID, reason)
    
    if err == nil {
        m.metrics.sessionsActive.Dec()
    }
    
    return err
}

// Periodic update активных сессий
func (m *SessionManager) updateSessionMetrics(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            count, err := m.sessionRepo.CountByState(ctx, StateRunning)
            if err != nil {
                continue
            }
            
            m.metrics.sessionsActive.Set(float64(count))
            
            // По типам
            byType, err := m.sessionRepo.CountByType(ctx)
            if err == nil {
                for runtimeType, count := range byType {
                    m.metrics.sessionsByType.WithLabelValues(runtimeType).Set(float64(count))
                }
            }
        }
    }
}
```

**Task Service:**

```go
package service

func (s *taskService) CreateTask(ctx context.Context, req CreateTaskRequest) (*Task, error) {
    task, err := s.taskServiceInternal(ctx, req)
    
    if err == nil {
        s.metrics.tasksTotal.Inc()
        s.metrics.tasksByRole.WithLabelValues(req.Role).Inc()
        s.metrics.queueSize.Inc()
    } else {
        s.metrics.errorsTotal.WithLabelValues("task_create", "task_service").Inc()
    }
    
    return task, err
}

func (s *taskService) UpdateTaskState(ctx context.Context, id int64, state string, reason string) error {
    oldState, err := s.taskRepo.GetState(ctx, id)
    if err != nil {
        return err
    }
    
    err = s.taskServiceInternal(ctx, id, state, reason)
    
    if err == nil {
        // Обновить gauge: уменьшить старый стейт, увеличить новый
        s.metrics.tasksByState.WithLabelValues(oldState).Dec()
        s.metrics.tasksByState.WithLabelValues(state).Inc()
        
        if state == "done" {
            // Задача завершена, посчитать длительность
            task, err := s.taskRepo.GetByID(ctx, id)
            if err == nil {
                duration := task.CompletedAt.Sub(task.CreatedAt).Seconds()
                s.metrics.tasksDuration.Observe(duration)
                s.metrics.queueSize.Dec()
            }
        }
    }
    
    return err
}

// Periodic update
func (s *taskService) updateTaskMetrics(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // По состояниям
            byState, err := s.taskRepo.CountByState(ctx)
            if err == nil {
                for state, count := range byState {
                    s.metrics.tasksByState.WithLabelValues(state).Set(float64(count))
                }
            }
            
            // Queue size
            queueSize, err := s.taskRepo.CountByStates(ctx, "pending", "in_progress")
            if err == nil {
                s.metrics.queueSize.Set(float64(queueSize))
            }
        }
    }
}
```

**Config Reload:**

```go
func (a *PiAdapter) UpdateConfig(ctx context.Context, runtimeRef string, newConfig *PiConfig) error {
    a.metrics.configReloadsTotal.Inc()
    
    err := a.updateConfigInternal(ctx, runtimeRef, newConfig)
    
    if err != nil {
        a.metrics.configReloadsFailed.Inc()
        a.metrics.errorsTotal.WithLabelValues("config_reload", "pi_adapter").Inc()
    }
    
    return err
}
```

**LLM Client:**

```go
package llm

func (c *Client) ChatCompletion(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
    start := time.Now()
    
    resp, err := c.chatCompletionInternal(ctx, req)
    
    duration := time.Since(start).Seconds()
    
    // Метрики
    status := "success"
    if err != nil {
        status = "error"
        c.metrics.llmRequestsTotal.WithLabelValues(req.Model, status).Inc()
        c.metrics.errorsTotal.WithLabelValues("llm_request", "llm_client").Inc()
    } else {
        c.metrics.llmRequestsTotal.WithLabelValues(req.Model, status).Inc()
        c.metrics.llmRequestDuration.Observe(duration)
        c.metrics.llmTokensTotal.WithLabelValues(req.Model, "prompt").Add(float64(resp.Usage.PromptTokens))
        c.metrics.llmTokensTotal.WithLabelValues(req.Model, "completion").Add(float64(resp.Usage.CompletionTokens))
    }
    
    return resp, err
}
```


______________________________________________________________________

## 2. Prometheus Configuration

**prometheus.yml:**

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'daemon'
    static_configs:
      - targets: ['daemon:9090']
    metrics_path: '/metrics'
    
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

# Alerting
alerting:
  alertmanagers:
    - static_configs:
      - targets:
        - alertmanager:9093

# Rule files
rule_files:
  - "alerts.yml"
```


______________________________________________________________________

## 3. Alerts

**alerts.yml:**

```yaml
groups:
  - name: daemon_alerts
    rules:
      # Sessions
      - alert: HighSessionFailureRate
        expr: |
          rate(daemon_errors_total{type="session_start"}[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High session failure rate"
          description: "Session failure rate is {{ $value }} per second"
      
      - alert: NoActiveSessions
        expr: |
          daemon_sessions_active == 0
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "No active sessions"
          description: "No sessions have been active for 10 minutes"
      
      # Tasks
      - alert: LargeQueueBacklog
        expr: |
          daemon_queue_size > 100
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Large task queue backlog"
          description: "Queue size is {{ $value }} tasks"
      
      - alert: StaleTasks
        expr: |
          daemon_tasks_by_state{state="in_progress"} > 0
        for: 2h
        labels:
          severity: warning
        annotations:
          summary: "Tasks stuck in progress"
          description: "Tasks have been in_progress for more than 2 hours"
      
      # Config reloads
      - alert: HighConfigReloadFailureRate
        expr: |
          rate(daemon_config_reloads_failed_total[5m]) > 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Config reloads are failing"
          description: "Config reload failure rate is {{ $value }} per second"
      
      # LLM
      - alert: HighLLMErrorRate
        expr: |
          rate(daemon_llm_requests_total{status="error"}[5m]) / rate(daemon_llm_requests_total[5m]) > 0.1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High LLM error rate"
          description: "LLM error rate is {{ $value | humanizePercentage }}"
      
      - alert: HighLLMLatency
        expr: |
          histogram_quantile(0.95, rate(daemon_llm_request_duration_seconds_bucket[5m])) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High LLM latency"
          description: "95th percentile LLM latency is {{ $value }} seconds"
      
      # Errors
      - alert: HighErrorRate
        expr: |
          rate(daemon_errors_total[5m]) > 1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High error rate"
          description: "Error rate is {{ $value }} per second"
```


______________________________________________________________________

## 4. Grafana Dashboards

### 4.1 Dashboard: Sessions Overview

**JSON (упрощённо):**

```json
{
  "dashboard": {
    "title": "Sessions Overview",
    "panels": [
      {
        "title": "Active Sessions",
        "type": "gauge",
        "targets": [
          {
            "expr": "daemon_sessions_active",
            "legendFormat": "Active"
          }
        ]
      },
      {
        "title": "Sessions by Runtime Type",
        "type": "piechart",
        "targets": [
          {
            "expr": "daemon_sessions_by_type",
            "legendFormat": "{{runtime_type}}"
          }
        ]
      },
      {
        "title": "Session Creation Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(daemon_sessions_total[5m])",
            "legendFormat": "Sessions/sec"
          }
        ]
      },
      {
        "title": "Session Duration (p50, p95, p99)",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.50, rate(daemon_session_duration_seconds_bucket[5m]))",
            "legendFormat": "p50"
          },
          {
            "expr": "histogram_quantile(0.95, rate(daemon_session_duration_seconds_bucket[5m]))",
            "legendFormat": "p95"
          },
          {
            "expr": "histogram_quantile(0.99, rate(daemon_session_duration_seconds_bucket[5m]))",
            "legendFormat": "p99"
          }
        ]
      }
    ]
  }
}
```


### 4.2 Dashboard: Tasks \& Queue

```json
{
  "dashboard": {
    "title": "Tasks & Queue",
    "panels": [
      {
        "title": "Queue Size",
        "type": "graph",
        "targets": [
          {
            "expr": "daemon_queue_size",
            "legendFormat": "Queue"
          }
        ]
      },
      {
        "title": "Tasks by State",
        "type": "stacked_graph",
        "targets": [
          {
            "expr": "daemon_tasks_by_state",
            "legendFormat": "{{state}}"
          }
        ]
      },
      {
        "title": "Task Creation Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(daemon_tasks_total[5m])",
            "legendFormat": "Tasks/sec"
          }
        ]
      },
      {
        "title": "Task Duration (p50, p95, p99)",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.50, rate(daemon_task_duration_seconds_bucket[5m]))",
            "legendFormat": "p50"
          },
          {
            "expr": "histogram_quantile(0.95, rate(daemon_task_duration_seconds_bucket[5m]))",
            "legendFormat": "p95"
          },
          {
            "expr": "histogram_quantile(0.99, rate(daemon_task_duration_seconds_bucket[5m]))",
            "legendFormat": "p99"
          }
        ]
      },
      {
        "title": "Tasks by Role",
        "type": "table",
        "targets": [
          {
            "expr": "rate(daemon_tasks_by_role[1h])",
            "legendFormat": "{{role}}"
          }
        ]
      }
    ]
  }
}
```


### 4.3 Dashboard: LLM Metrics

```json
{
  "dashboard": {
    "title": "LLM Metrics",
    "panels": [
      {
        "title": "LLM Request Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(daemon_llm_requests_total[5m])",
            "legendFormat": "{{model}} - {{status}}"
          }
        ]
      },
      {
        "title": "LLM Latency (p50, p95, p99)",
        "type": "graph",
        "targets": [
          {
            "expr": "histogram_quantile(0.50, rate(daemon_llm_request_duration_seconds_bucket[5m]))",
            "legendFormat": "p50"
          },
          {
            "expr": "histogram_quantile(0.95, rate(daemon_llm_request_duration_seconds_bucket[5m]))",
            "legendFormat": "p95"
          },
          {
            "expr": "histogram_quantile(0.99, rate(daemon_llm_request_duration_seconds_bucket[5m]))",
            "legendFormat": "p99"
          }
        ]
      },
      {
        "title": "Token Usage",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(daemon_llm_tokens_total{type=\"prompt\"}[5m])",
            "legendFormat": "{{model}} - prompt"
          },
          {
            "expr": "rate(daemon_llm_tokens_total{type=\"completion\"}[5m])",
            "legendFormat": "{{model}} - completion"
          }
        ]
      },
      {
        "title": "LLM Error Rate",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(daemon_llm_requests_total{status=\"error\"}[5m]) / rate(daemon_llm_requests_total[5m])",
            "legendFormat": "{{model}}"
          }
        ]
      }
    ]
  }
}
```


### 4.4 Dashboard: Errors \& Config Reloads

```json
{
  "dashboard": {
    "title": "Errors & Config",
    "panels": [
      {
        "title": "Error Rate by Type",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(daemon_errors_total[5m])",
            "legendFormat": "{{type}} - {{component}}"
          }
        ]
      },
      {
        "title": "Config Reloads",
        "type": "graph",
        "targets": [
          {
            "expr": "rate(daemon_config_reloads_total[5m])",
            "legendFormat": "Total"
          },
          {
            "expr": "rate(daemon_config_reloads_failed_total[5m])",
            "legendFormat": "Failed"
          }
        ]
      },
      {
        "title": "Config Reload Success Rate",
        "type": "gauge",
        "targets": [
          {
            "expr": "rate(daemon_config_reloads_total[5m]) / (rate(daemon_config_reloads_total[5m]) + rate(daemon_config_reloads_failed_total[5m]))",
            "legendFormat": "Success Rate"
          }
        ]
      }
    ]
  }
}
```


______________________________________________________________________

## 5. Docker Compose для Monitoring Stack

**docker-compose.yml:**

```yaml
version: '3.8'

services:
  daemon:
    build: .
    ports:
      - "8080:8080"  # API
      - "9090:9090"  # Metrics
    environment:
      - DATABASE_URL=postgres://user:pass@db:5432/daemon
    depends_on:
      - db
  
  db:
    image: postgres:15
    environment:
      - POSTGRES_USER=user
      - POSTGRES_PASSWORD=pass
      - POSTGRES_DB=daemon
    volumes:
      - postgres_data:/var/lib/postgresql/data
  
  prometheus:
    image: prom/prometheus:v2.45.0
    volumes:
      - ./prometheus.yml:/etc/prometheus/prometheus.yml
      - ./alerts.yml:/etc/prometheus/alerts.yml
      - prometheus_data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/etc/prometheus/console_libraries'
      - '--web.console.templates=/etc/prometheus/consoles'
      - '--web.enable-lifecycle'
    ports:
      - "9090:9090"
    depends_on:
      - daemon
  
  grafana:
    image: grafana/grafana:10.0.0
    volumes:
      - grafana_data:/var/lib/grafana
      - ./grafana/dashboards:/etc/grafana/provisioning/dashboards
      - ./grafana/datasources:/etc/grafana/provisioning/datasources
    environment:
      - GF_SECURITY_ADMIN_USER=admin
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_USERS_ALLOW_SIGN_UP=false
    ports:
      - "3000:3000"
    depends_on:
      - prometheus
  
  alertmanager:
    image: prom/alertmanager:v0.25.0
    volumes:
      - ./alertmanager.yml:/etc/alertmanager/alertmanager.yml
      - alertmanager_data:/alertmanager
    ports:
      - "9093:9093"
    depends_on:
      - prometheus

volumes:
  postgres_data:
  prometheus_data:
  grafana_data:
  alertmanager_data:
```


______________________________________________________________________

## 6. Полезные PromQL Queries

```promql
# Active sessions
daemon_sessions_active

# Session creation rate (per second)
rate(daemon_sessions_total[5m])

# Average session duration
rate(daemon_session_duration_seconds_sum[5m]) / rate(daemon_session_duration_seconds_count[5m])

# Task queue size
daemon_queue_size

# Tasks by state
daemon_tasks_by_state

# Task completion rate
rate(daemon_tasks_total{state="done"}[5m])

# 95th percentile task duration
histogram_quantile(0.95, rate(daemon_task_duration_seconds_bucket[5m]))

# LLM request rate by model
rate(daemon_llm_requests_total[5m])

# LLM error rate percentage
rate(daemon_llm_requests_total{status="error"}[5m]) / rate(daemon_llm_requests_total[5m]) * 100

# Token usage per minute
rate(daemon_llm_tokens_total[5m]) * 60

# Config reload success rate
rate(daemon_config_reloads_total[5m]) / (rate(daemon_config_reloads_total[5m]) + rate(daemon_config_reloads_failed_total[5m])) * 100

# Error rate by component
sum by (component) (rate(daemon_errors_total[5m]))
```


______________________________________________________________________

## Рекомендации

### 1. **Instrument Everything**

- Все важные операции: count, duration, errors
- Business metrics: tasks, sessions, queue
- Technical metrics: latency, throughput, errors


### 2. **Use Labels Wisely**

- Не слишком много label combinations (cardinality explosion)
- Полезные labels: state, type, role, model


### 3. **Set Up Alerts**

- Critical: errors, failures
- Warning: backlogs, latency
- Info: unusual patterns


### 4. **Review Dashboards Regularly**

- Что в норме, что нет
- Trends over time
- Capacity planning


### 5. **Cost Monitoring**

- LLM tokens = деньги
- Track by model, by project
- Set budgets and alerts

