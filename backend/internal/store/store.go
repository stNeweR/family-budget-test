package store

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/domain"
)

type Store interface {
	// Ping verifies the database is reachable.
	Ping(ctx context.Context) error

	// UpsertUser creates or updates a family member.
	UpsertUser(ctx context.Context, user domain.User) error
	// ListUsers returns all known family members ordered by name.
	ListUsers(ctx context.Context) ([]domain.User, error)

	// CreateExpense inserts a new expense and fills ID/created/updated fields.
	CreateExpense(ctx context.Context, expense *domain.Expense) error
	// GetExpense returns a single expense or domain.ErrNotFound.
	GetExpense(ctx context.Context, id uuid.UUID) (domain.Expense, error)
	// ListExpenses returns expenses matching the filter ordered by spent_at desc.
	ListExpenses(ctx context.Context, filter domain.ExpenseFilter) ([]domain.Expense, error)
	// UpdateExpense applies a partial update and returns the fresh row.
	UpdateExpense(ctx context.Context, id uuid.UUID, patch domain.ExpensePatch) (domain.Expense, error)
	// DeleteExpense removes an expense.
	DeleteExpense(ctx context.Context, id uuid.UUID) error
}

// ErrNotFound is re-exported so callers can rely on a single sentinel.
var ErrNotFound = domain.ErrNotFound

// IsNotFound reports whether the error means "no such row".
func IsNotFound(err error) bool {
	return errors.Is(err, domain.ErrNotFound)
}
