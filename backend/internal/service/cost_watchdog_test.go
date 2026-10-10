package service_test

import (
	"context"
	"encoding/json"
	"testing"

	"daemon/internal/repository"
	"daemon/internal/service"
)

// ---------- P2.14: budget-порог → watchdog-алерт (dedup) ----------

func budgetEventCount(t *testing.T, e *sessionEnv) int {
	t.Helper()
	events, _, err := e.stores.Watchdog.List(e.ctx, e.db, repository.AlertFilter{Limit: 200})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	n := 0
	for _, ev := range events {
		if ev.EventType == "budget" {
			n++
		}
	}
	return n
}

func budgetEnv(t *testing.T, e *sessionEnv, thresholdCents string) {
	t.Helper()
	// команда-дом для глобального budget-алерта (team_id FK)
	e.teamWithRole(t, "budget-team")
	e.wd.Bus = service.NewEventBus()
	cfg, _ := setupConfigService(t, nil, "")
	if thresholdCents != "" {
		if _, err := cfg.SetOverride(e.ctx, "cost.budget_alert_cents", json.RawMessage(thresholdCents)); err != nil {
			t.Fatalf("set budget: %v", err)
		}
	}
	cs := service.NewCostService(cfg)
	e.wd.CostSvc = cs
}

func TestWatchdogBudgetAlert_Fires(t *testing.T) {
	e := setupSessionEnv(t)
	budgetEnv(t, e, "100") // порог $1.00
	c := 2.0               // $2.00 = 200 центов > 100
	e.wd.TotalCost = func(ctx context.Context) *service.CostInfo {
		return &service.CostInfo{CostUSD: &c, HasData: true, Estimate: true}
	}
	e.wd.Scan(e.ctx)
	if n := budgetEventCount(t, e); n != 1 {
		t.Fatalf("want 1 budget event, got %d", n)
	}
}

func TestWatchdogBudgetAlert_Dedup(t *testing.T) {
	e := setupSessionEnv(t)
	budgetEnv(t, e, "100")
	c := 2.0
	e.wd.TotalCost = func(ctx context.Context) *service.CostInfo {
		return &service.CostInfo{CostUSD: &c, HasData: true, Estimate: true}
	}
	e.wd.Scan(e.ctx)
	e.wd.Scan(e.ctx) // cooldown → не спамит
	if n := budgetEventCount(t, e); n != 1 {
		t.Fatalf("want 1 budget event after dedup, got %d", n)
	}
}

func TestWatchdogBudgetAlert_BelowThreshold(t *testing.T) {
	e := setupSessionEnv(t)
	budgetEnv(t, e, "100")
	c := 0.5 // $0.50 = 50 центов < 100
	e.wd.TotalCost = func(ctx context.Context) *service.CostInfo {
		return &service.CostInfo{CostUSD: &c, HasData: true, Estimate: true}
	}
	e.wd.Scan(e.ctx)
	if n := budgetEventCount(t, e); n != 0 {
		t.Fatalf("want 0 budget events (below threshold), got %d", n)
	}
}

func TestWatchdogBudgetAlert_NoData(t *testing.T) {
	e := setupSessionEnv(t)
	budgetEnv(t, e, "100")
	e.wd.TotalCost = func(ctx context.Context) *service.CostInfo { return service.NoDataCost() }
	e.wd.Scan(e.ctx)
	if n := budgetEventCount(t, e); n != 0 {
		t.Fatalf("want 0 budget events (no data), got %d", n)
	}
}

func TestWatchdogBudgetAlert_Disabled(t *testing.T) {
	e := setupSessionEnv(t)
	budgetEnv(t, e, "") // default 0 → выключен
	c := 5.0
	e.wd.TotalCost = func(ctx context.Context) *service.CostInfo {
		return &service.CostInfo{CostUSD: &c, HasData: true, Estimate: true}
	}
	e.wd.Scan(e.ctx)
	if n := budgetEventCount(t, e); n != 0 {
		t.Fatalf("want 0 (disabled), got %d", n)
	}
}
