package assertions

import (
	"fmt"
	"strings"
)

// TokenBudgetAssertion enforces prompt/completion/total token ceilings.
// This is a hard gate because cost and latency regressions should block.
type TokenBudgetAssertion struct{}

func (a *TokenBudgetAssertion) Type() string { return "token_budget" }
func (a *TokenBudgetAssertion) IsSoft() bool { return false }

type tokenUsage struct {
	index      int
	model      string
	prompt     int
	completion int
}

func (a *TokenBudgetAssertion) Evaluate(ctx Context) Result {
	scope, hasScope := specString(ctx.Spec, "scope")
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = "total"
	}
	if scope != "total" && scope != "input_only" && scope != "output_only" {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Message:       fmt.Sprintf("token_budget: unsupported scope %q (supported: input_only, output_only, total)", scope),
			DocketHint:    "assertion.spec_invalid",
		}
	}

	maxPrompt, hasPrompt := specInt(ctx.Spec, "max_prompt_tokens")
	maxCompletion, hasCompletion := specInt(ctx.Spec, "max_completion_tokens")
	maxTotal, hasTotal := specInt(ctx.Spec, "max_total_tokens")
	maxTokens, hasMaxTokens := specInt(ctx.Spec, "max_tokens")

	modelCalls := []tokenUsage{}

	promptUsed := 0
	completionUsed := 0
	for idx, event := range ctx.ToolTrace {
		if event.EventType != "model_call" || event.ModelCall == nil {
			continue
		}
		prompt := 0
		completion := 0
		if event.ModelCall.PromptTokens > 0 {
			prompt = event.ModelCall.PromptTokens
			promptUsed += prompt
		}
		if event.ModelCall.CompletionTokens > 0 {
			completion = event.ModelCall.CompletionTokens
			completionUsed += completion
		}
		if prompt > 0 || completion > 0 {
			modelCalls = append(modelCalls, tokenUsage{
				index:      idx + 1,
				model:      strings.TrimSpace(event.ModelCall.Model),
				prompt:     prompt,
				completion: completion,
			})
		}
	}
	totalUsed := promptUsed + completionUsed

	if len(modelCalls) == 0 {
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Message:       "token_budget skipped: model token counts unavailable",
		}
	}

	if hasScope {
		budget, ok := resolveScopedBudget(scope, hasPrompt, maxPrompt, hasCompletion, maxCompletion, hasTotal, maxTotal, hasMaxTokens, maxTokens)
		if !ok {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Message:       fmt.Sprintf("token_budget: scope %q requires corresponding budget field", scope),
				DocketHint:    "assertion.spec_invalid",
			}
		}
		actual := scopedTokenValue(scope, promptUsed, completionUsed, totalUsed)
		if actual > budget {
			return Result{
				AssertionType: a.Type(),
				Passed:        false,
				Expected:      fmt.Sprintf("%s tokens <= %d", scopeLabel(scope), budget),
				Actual:        fmt.Sprintf("%s tokens = %d", scopeLabel(scope), actual),
				Message: fmt.Sprintf(
					"token_budget: %s tokens %d exceed budget %d; model calls: %s",
					scopeLabel(scope),
					actual,
					budget,
					modelCallUsageSummary(modelCalls),
				),
				DocketHint: "cost.token_budget_exceeded",
			}
		}
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Message: fmt.Sprintf(
				"token_budget: %s tokens %d within budget %d",
				scopeLabel(scope),
				actual,
				budget,
			),
		}
	}

	// Backward-compatible behavior when scope is omitted.
	if hasPrompt && promptUsed > maxPrompt {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("prompt_tokens <= %d", maxPrompt),
			Actual:        fmt.Sprintf("prompt_tokens = %d", promptUsed),
			Message: fmt.Sprintf(
				"token budget exceeded: prompt tokens %d > %d; model calls: %s",
				promptUsed,
				maxPrompt,
				modelCallUsageSummary(modelCalls),
			),
			DocketHint: "cost.token_budget_exceeded",
		}
	}
	if hasCompletion && completionUsed > maxCompletion {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("completion_tokens <= %d", maxCompletion),
			Actual:        fmt.Sprintf("completion_tokens = %d", completionUsed),
			Message: fmt.Sprintf(
				"token budget exceeded: completion tokens %d > %d; model calls: %s",
				completionUsed,
				maxCompletion,
				modelCallUsageSummary(modelCalls),
			),
			DocketHint: "cost.token_budget_exceeded",
		}
	}
	if hasTotal && totalUsed > maxTotal {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("total_tokens <= %d", maxTotal),
			Actual:        fmt.Sprintf("total_tokens = %d", totalUsed),
			Message: fmt.Sprintf(
				"token budget exceeded: total tokens %d > %d; model calls: %s",
				totalUsed,
				maxTotal,
				modelCallUsageSummary(modelCalls),
			),
			DocketHint: "cost.token_budget_exceeded",
		}
	}
	if hasMaxTokens && totalUsed > maxTokens {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("total_tokens <= %d", maxTokens),
			Actual:        fmt.Sprintf("total_tokens = %d", totalUsed),
			Message: fmt.Sprintf(
				"token budget exceeded: total tokens %d > %d; model calls: %s",
				totalUsed,
				maxTokens,
				modelCallUsageSummary(modelCalls),
			),
			DocketHint: "cost.token_budget_exceeded",
		}
	}

	limitsConfigured := hasPrompt || hasCompletion || hasTotal || hasMaxTokens
	if !limitsConfigured {
		return Result{
			AssertionType: a.Type(),
			Passed:        true,
			Message:       "token budget assertion has no configured limits",
		}
	}

	return Result{
		AssertionType: a.Type(),
		Passed:        true,
		Message: fmt.Sprintf(
			"token budget within limits (prompt=%d completion=%d total=%d)",
			promptUsed,
			completionUsed,
			totalUsed,
		),
	}
}

func resolveScopedBudget(scope string, hasPrompt bool, maxPrompt int, hasCompletion bool, maxCompletion int, hasTotal bool, maxTotal int, hasMaxTokens bool, maxTokens int) (int, bool) {
	switch scope {
	case "input_only":
		if hasPrompt {
			return maxPrompt, true
		}
		if hasMaxTokens {
			return maxTokens, true
		}
	case "output_only":
		if hasCompletion {
			return maxCompletion, true
		}
		if hasMaxTokens {
			return maxTokens, true
		}
	case "total":
		if hasTotal {
			return maxTotal, true
		}
		if hasMaxTokens {
			return maxTokens, true
		}
	}
	return 0, false
}

func scopedTokenValue(scope string, prompt, completion, total int) int {
	switch scope {
	case "input_only":
		return prompt
	case "output_only":
		return completion
	default:
		return total
	}
}

func scopeLabel(scope string) string {
	switch scope {
	case "input_only":
		return "input"
	case "output_only":
		return "output"
	default:
		return "total"
	}
}

func modelCallUsageSummary(calls []tokenUsage) string {
	parts := make([]string, 0, len(calls))
	for _, call := range calls {
		model := call.model
		if model == "" {
			model = "unknown"
		}
		parts = append(parts, fmt.Sprintf("#%d(%s in=%d out=%d)", call.index, model, call.prompt, call.completion))
	}
	return strings.Join(parts, ", ")
}
