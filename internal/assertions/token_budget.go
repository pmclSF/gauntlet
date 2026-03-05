package assertions

import "fmt"

// TokenBudgetAssertion enforces prompt/completion/total token ceilings.
// This is a hard gate because cost and latency regressions should block.
type TokenBudgetAssertion struct{}

func (a *TokenBudgetAssertion) Type() string { return "token_budget" }
func (a *TokenBudgetAssertion) IsSoft() bool { return false }

func (a *TokenBudgetAssertion) Evaluate(ctx Context) Result {
	maxPrompt, hasPrompt := specInt(ctx.Spec, "max_prompt_tokens")
	maxCompletion, hasCompletion := specInt(ctx.Spec, "max_completion_tokens")
	maxTotal, hasTotal := specInt(ctx.Spec, "max_total_tokens")

	promptUsed := 0
	completionUsed := 0
	for _, event := range ctx.ToolTrace {
		if event.EventType != "model_call" || event.ModelCall == nil {
			continue
		}
		if event.ModelCall.PromptTokens > 0 {
			promptUsed += event.ModelCall.PromptTokens
		}
		if event.ModelCall.CompletionTokens > 0 {
			completionUsed += event.ModelCall.CompletionTokens
		}
	}
	totalUsed := promptUsed + completionUsed

	if hasPrompt && promptUsed > maxPrompt {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("prompt_tokens <= %d", maxPrompt),
			Actual:        fmt.Sprintf("prompt_tokens = %d", promptUsed),
			Message:       fmt.Sprintf("token budget exceeded: prompt tokens %d > %d", promptUsed, maxPrompt),
			DocketHint:    "cost.token_budget_exceeded",
		}
	}
	if hasCompletion && completionUsed > maxCompletion {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("completion_tokens <= %d", maxCompletion),
			Actual:        fmt.Sprintf("completion_tokens = %d", completionUsed),
			Message:       fmt.Sprintf("token budget exceeded: completion tokens %d > %d", completionUsed, maxCompletion),
			DocketHint:    "cost.token_budget_exceeded",
		}
	}
	if hasTotal && totalUsed > maxTotal {
		return Result{
			AssertionType: a.Type(),
			Passed:        false,
			Expected:      fmt.Sprintf("total_tokens <= %d", maxTotal),
			Actual:        fmt.Sprintf("total_tokens = %d", totalUsed),
			Message:       fmt.Sprintf("token budget exceeded: total tokens %d > %d", totalUsed, maxTotal),
			DocketHint:    "cost.token_budget_exceeded",
		}
	}

	limitsConfigured := hasPrompt || hasCompletion || hasTotal
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
