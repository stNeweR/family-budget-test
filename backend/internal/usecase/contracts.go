package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/entity"
)

type Expense interface {
	List(ctx context.Context, filter entity.ExpenseFilter) ([]entity.Expense, error)

	ListFamily(ctx context.Context, headID int64, filter entity.ExpenseFilter) ([]entity.Expense, error)

	Create(ctx context.Context, expense entity.Expense) (entity.Expense, error)

	Get(ctx context.Context, id uuid.UUID) (entity.Expense, error)

	Update(ctx context.Context, id uuid.UUID, patch entity.ExpensePatch) (entity.Expense, error)

	Delete(ctx context.Context, id uuid.UUID) error

	Owned(ctx context.Context, userID int64, id uuid.UUID) (entity.Expense, error)

	DeleteOwned(ctx context.Context, userID int64, id uuid.UUID) (entity.Expense, error)

	Total(ctx context.Context, filter entity.ExpenseFilter) (float64, int, error)
}

type Family interface {
	EnsureUser(ctx context.Context, user entity.User) (entity.User, error)

	Start(ctx context.Context, user entity.User, code string) (entity.StartResult, error)

	Invite(ctx context.Context, userID int64) (entity.InviteResult, error)

	ListUsers(ctx context.Context) ([]entity.User, error)

	ListFamilyUsers(ctx context.Context, headID int64) ([]entity.User, error)
}

type Agent interface {
	Handle(ctx context.Context, user entity.User, text string) (string, error)
}
