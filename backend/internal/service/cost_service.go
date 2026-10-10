package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// P2.14 — Cost tracking: оценка стоимости LLM-сессий по токенам × цене модели.
// Честность: нет токенов (0/nil) ИЛИ нет цены модели → «нет данных» (null), НЕ 0.
// estimate=true всегда (оценка по заявленным ценам, не факт).

// ModelPrice — цена модели (USD за мега-токен, 1e6).
type ModelPrice struct {
	InUSDPerMtok  float64 `json:"in_usd_per_mtok"`
	OutUSDPerMtok float64 `json:"out_usd_per_mtok"`
}

// CostInfo — оценка стоимости. CostUSD=null + HasData=false = «нет данных» (не 0).
type CostInfo struct {
	CostUSD  *float64 `json:"cost_usd"`
	HasData  bool     `json:"has_data"`
	Estimate bool     `json:"estimate"`
}

// NoDataCost — «нет данных» (не 0); estimate (оценка).
func NoDataCost() *CostInfo { return &CostInfo{CostUSD: nil, HasData: false, Estimate: true} }

// CostService — расчёт стоимости по токенам + ценам из config (cost.model_prices).
type CostService struct {
	cfg *ConfigService
}

// NewCostService — cfg может быть nil (тогда все расчёты → «нет данных»).
func NewCostService(cfg *ConfigService) *CostService { return &CostService{cfg: cfg} }

// Prices — cost.model_prices (JSON-объект) → map[model]ModelPrice.
// Пусто/{} → пустая карта (цены не заданы).
func (s *CostService) Prices(ctx context.Context) (map[string]ModelPrice, error) {
	out := map[string]ModelPrice{}
	if s.cfg == nil {
		return out, nil
	}
	raw, err := s.cfg.ResolveString(ctx, "cost.model_prices")
	if err != nil {
		return nil, err
	}
	t := strings.TrimSpace(raw)
	if t == "" || t == "{}" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(t), &out); err != nil {
		return nil, NewValidation(fmt.Sprintf("cost.model_prices: invalid JSON object: %v", err))
	}
	return out, nil
}

// BudgetAlertCents — порог бюджета в центах (0 = выключен).
func (s *CostService) BudgetAlertCents(ctx context.Context) (int64, error) {
	if s.cfg == nil {
		return 0, nil
	}
	n, err := s.cfg.ResolveInt(ctx, "cost.budget_alert_cents")
	if err != nil {
		return 0, err
	}
	return int64(n), nil
}

// SessionCost — оценка стоимости сессии: in_tokens×in_price/1e6 +
// out_tokens×out_price/1e6. «Нет данных» (null, не 0) при нетокенов (0)
// ИЛИ нет имени модели ИЛИ модели нет в ценах.
func (s *CostService) SessionCost(ctx context.Context, inTokens, outTokens *int64, model string) *CostInfo {
	in := int64(0)
	if inTokens != nil {
		in = *inTokens
	}
	out := int64(0)
	if outTokens != nil {
		out = *outTokens
	}
	if in == 0 && out == 0 {
		return NoDataCost() // нет usage
	}
	if model == "" {
		return NoDataCost()
	}
	prices, err := s.Prices(ctx)
	if err != nil {
		return NoDataCost()
	}
	mp, ok := prices[model]
	if !ok {
		return NoDataCost() // модели нет в ценах
	}
	cost := float64(in)*mp.InUSDPerMtok/1e6 + float64(out)*mp.OutUSDPerMtok/1e6
	return &CostInfo{CostUSD: &cost, HasData: true, Estimate: true}
}

// SumCost — агрегация (per task/project): сумма cost сессий. Если ни одной
// с данными → «нет данных» (null, не 0).
func (s *CostService) SumCost(items []*CostInfo) *CostInfo {
	total := 0.0
	has := false
	for _, it := range items {
		if it != nil && it.HasData && it.CostUSD != nil {
			total += *it.CostUSD
			has = true
		}
	}
	if !has {
		return NoDataCost()
	}
	total = math.Round(total*1e6) / 1e6 // уменьшаем float-шум
	return &CostInfo{CostUSD: &total, HasData: true, Estimate: true}
}
