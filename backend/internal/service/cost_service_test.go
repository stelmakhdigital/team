package service_test

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"daemon/internal/service"
)

// helper: ConfigService + model_prices override → CostService.
func costEnv(t *testing.T, modelPricesJSON string) *service.CostService {
	t.Helper()
	cfg, _ := setupConfigService(t, nil, "")
	if modelPricesJSON != "" {
		// StrType-override = JSON-строка (внутри — JSON-объект цен).
		strVal, _ := json.Marshal(modelPricesJSON)
		if _, err := cfg.SetOverride(context.Background(), "cost.model_prices", json.RawMessage(strVal)); err != nil {
			t.Fatalf("set model_prices: %v", err)
		}
	}
	return service.NewCostService(cfg)
}

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCostSessionCost_Computed(t *testing.T) {
	cs := costEnv(t, `{"test-model":{"in_usd_per_mtok":1.0,"out_usd_per_mtok":3.0}}`)
	in, out := int64(1_000_000), int64(2_000_000)
	// 1e6*1/1e6 + 2e6*3/1e6 = 1 + 6 = 7
	ci := cs.SessionCost(context.Background(), &in, &out, "test-model")
	if !ci.HasData || ci.CostUSD == nil {
		t.Fatalf("want has_data, got %+v", ci)
	}
	approx(t, *ci.CostUSD, 7.0)
	if !ci.Estimate {
		t.Fatalf("want estimate=true")
	}
}

func TestCostSessionCost_NoUsage_NoData(t *testing.T) {
	cs := costEnv(t, `{"test-model":{"in_usd_per_mtok":1.0,"out_usd_per_mtok":3.0}}`)
	// 0 токенов → «нет данных» (не 0)
	for _, in := range []int64{0} {
		for _, out := range []int64{0} {
			ci := cs.SessionCost(context.Background(), &in, &out, "test-model")
			if ci.HasData || ci.CostUSD != nil {
				t.Fatalf("want no-data (null), got %+v", ci)
			}
			if !ci.Estimate {
				t.Fatalf("want estimate=true even no-data")
			}
		}
	}
	// nil pointers → тоже нет данных
	ci := cs.SessionCost(context.Background(), nil, nil, "test-model")
	if ci.HasData || ci.CostUSD != nil {
		t.Fatalf("nil tokens: want no-data, got %+v", ci)
	}
}

func TestCostSessionCost_NoPrice_NoData(t *testing.T) {
	// цены не заданы (пусто)
	cs := costEnv(t, "")
	in, out := int64(1_000_000), int64(2_000_000)
	ci := cs.SessionCost(context.Background(), &in, &out, "test-model")
	if ci.HasData || ci.CostUSD != nil {
		t.Fatalf("no prices: want no-data, got %+v", ci)
	}
	// модель не в списке
	cs2 := costEnv(t, `{"other-model":{"in_usd_per_mtok":1.0,"out_usd_per_mtok":1.0}}`)
	ci2 := cs2.SessionCost(context.Background(), &in, &out, "test-model")
	if ci2.HasData || ci2.CostUSD != nil {
		t.Fatalf("model not in prices: want no-data, got %+v", ci2)
	}
	// нет имени модели
	cs3 := costEnv(t, `{"test-model":{"in_usd_per_mtok":1.0,"out_usd_per_mtok":1.0}}`)
	ci3 := cs3.SessionCost(context.Background(), &in, &out, "")
	if ci3.HasData || ci3.CostUSD != nil {
		t.Fatalf("no model name: want no-data, got %+v", ci3)
	}
}

func TestCostSumCost(t *testing.T) {
	cs := costEnv(t, `{"m":{"in_usd_per_mtok":1.0,"out_usd_per_mtok":0.0}}`)
	inA, inB := int64(1_000_000), int64(2_000_000)
	// A: 1e6*1/1e6 = 1; B: 2e6*1/1e6 = 2; no-data: skip
	a := cs.SessionCost(context.Background(), &inA, nil, "m")
	b := cs.SessionCost(context.Background(), &inB, nil, "m")
	nd := service.NoDataCost()
	sum := cs.SumCost([]*service.CostInfo{a, b, nd})
	if !sum.HasData || sum.CostUSD == nil {
		t.Fatalf("want sum has_data, got %+v", sum)
	}
	approx(t, *sum.CostUSD, 3.0)
	if !sum.Estimate {
		t.Fatalf("want estimate=true")
	}
}

func TestCostSumCost_AllNoData(t *testing.T) {
	cs := costEnv(t, "")
	sum := cs.SumCost([]*service.CostInfo{service.NoDataCost(), service.NoDataCost()})
	if sum.HasData || sum.CostUSD != nil {
		t.Fatalf("all no-data: want null (not 0), got %+v", sum)
	}
}

func TestCostBudgetAlertCents(t *testing.T) {
	cfg, _ := setupConfigService(t, nil, "")
	cs := service.NewCostService(cfg)
	// default 0
	n, err := cs.BudgetAlertCents(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("default budget = %d (err %v), want 0", n, err)
	}
	if _, err := cfg.SetOverride(context.Background(), "cost.budget_alert_cents", json.RawMessage(`500`)); err != nil {
		t.Fatalf("set budget: %v", err)
	}
	n, err = cs.BudgetAlertCents(context.Background())
	if err != nil || n != 500 {
		t.Fatalf("budget = %d (err %v), want 500", n, err)
	}
}
