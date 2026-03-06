package assertions

import (
	"fmt"
	"sort"
	"strings"
)

// CostBudgetAssertion enforces per-scenario cost ceilings using static pricing.
type CostBudgetAssertion struct{}

func (a *CostBudgetAssertion) Type() string { return "cost_budget" }
func (a *CostBudgetAssertion) IsSoft() bool { return false }

type modelPricing struct {
	InputPer1K  float64
	OutputPer1K float64
}

type scenarioCharge struct {
	model      string
	prompt     int
	completion int
	cost       float64
}

var staticPricingTable = map[string]modelPricing{
	"gpt-4o":                   {InputPer1K: 0.005, OutputPer1K: 0.015},
	"gpt-4o-mini":              {InputPer1K: 0.00015, OutputPer1K: 0.0006},
	"gpt-4.1":                  {InputPer1K: 0.01, OutputPer1K: 0.03},
	"claude-3-5-sonnet":        {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-3-5-sonnet-latest": {InputPer1K: 0.003, OutputPer1K: 0.015},
	"claude-opus-4":            {InputPer1K: 0.015, OutputPer1K: 0.075},
}

func (a *CostBudgetAssertion) Evaluate(ctx Context) Result {
	usdMax, ok := specFloat(ctx.Spec, "usd_max")
	if !ok || usdMax < 0 {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       "cost_budget: missing required field 'usd_max'",
			DocketHint:    "assertion.spec_invalid",
		}
	}

	charges := []scenarioCharge{}
	missingCounts := 0
	unknownModels := map[string]bool{}
	for _, event := range ctx.ToolTrace {
		if event.EventType != "model_call" || event.ModelCall == nil {
			continue
		}
		prompt := event.ModelCall.PromptTokens
		completion := event.ModelCall.CompletionTokens
		if prompt <= 0 && completion <= 0 {
			missingCounts++
			continue
		}
		model := strings.TrimSpace(event.ModelCall.Model)
		pricing, found := resolvePricing(model)
		if !found {
			unknownModels[strings.ToLower(model)] = true
			continue
		}
		cost := (float64(prompt)/1000.0)*pricing.InputPer1K + (float64(completion)/1000.0)*pricing.OutputPer1K
		charges = append(charges, scenarioCharge{
			model:      model,
			prompt:     prompt,
			completion: completion,
			cost:       cost,
		})
	}

	if len(unknownModels) > 0 {
		models := make([]string, 0, len(unknownModels))
		for model := range unknownModels {
			models = append(models, model)
		}
		sort.Strings(models)
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       fmt.Sprintf("cost_budget: unknown model pricing for %s", strings.Join(models, ", ")),
			DocketHint:    "assertion.spec_invalid",
		}
	}

	total := 0.0
	for _, c := range charges {
		total += c.cost
	}
	if len(charges) == 0 && missingCounts > 0 {
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Message:       "cost_budget: skipped because model token counts were unavailable",
		}
	}

	if total > usdMax {
		top := dominantCharge(charges)
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("scenario cost <= $%.3f", usdMax),
			Actual:        fmt.Sprintf("scenario cost = $%.3f", total),
			Message: fmt.Sprintf(
				"cost_budget: scenario cost $%.3f exceeds budget of $%.3f (model: %s, tokens: %d in / %d out)",
				total,
				usdMax,
				top.model,
				top.prompt,
				top.completion,
			),
			DocketHint: "cost.budget_exceeded",
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message:       fmt.Sprintf("cost_budget: scenario cost $%.3f within budget $%.3f", total, usdMax),
	}
}

func resolvePricing(model string) (modelPricing, bool) {
	key := strings.ToLower(strings.TrimSpace(model))
	if key == "" {
		return modelPricing{}, false
	}
	if p, ok := staticPricingTable[key]; ok {
		return p, true
	}
	// Prefix fallback for versioned model names.
	for candidate, pricing := range staticPricingTable {
		if strings.HasPrefix(key, candidate) {
			return pricing, true
		}
	}
	return modelPricing{}, false
}

func dominantCharge(charges []scenarioCharge) scenarioCharge {
	if len(charges) == 0 {
		return scenarioCharge{model: "unknown"}
	}
	top := charges[0]
	for _, c := range charges[1:] {
		if c.cost > top.cost {
			top = c
		}
	}
	return top
}
