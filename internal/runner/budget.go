package runner

import (
	"context"
	"time"
)

// BudgetEnforcer tracks wall-clock time against a budget.
// It stops after the current scenario completes, never mid-execution.
type BudgetEnforcer struct {
	BudgetMs  int64
	StartTime time.Time
}

// NewBudgetEnforcer creates a new enforcer with the given budget.
func NewBudgetEnforcer(budgetMs int64) *BudgetEnforcer {
	return &BudgetEnforcer{
		BudgetMs:  budgetMs,
		StartTime: time.Now(),
	}
}

// Exceeded returns true if the budget has been exceeded.
func (b *BudgetEnforcer) Exceeded() bool {
	return time.Since(b.StartTime).Milliseconds() >= b.BudgetMs
}

// RemainingMs returns remaining budget in milliseconds.
func (b *BudgetEnforcer) RemainingMs() int64 {
	elapsed := time.Since(b.StartTime).Milliseconds()
	remaining := b.BudgetMs - elapsed
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ContextWithBudget returns a context that cancels when the budget is exceeded.
func (b *BudgetEnforcer) ContextWithBudget(parent context.Context) (context.Context, context.CancelFunc) {
	remaining := time.Duration(b.RemainingMs()) * time.Millisecond
	return context.WithTimeout(parent, remaining)
}
