package repo

import (
	"context"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/entity"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type ExpenseRepo interface {
	CreateExpense(ctx context.Context, expense *entity.Expense) error

	GetExpense(ctx context.Context, id uuid.UUID) (entity.Expense, error)

	ListExpenses(ctx context.Context, filter entity.ExpenseFilter) ([]entity.Expense, error)

	ListFamilyExpenses(ctx context.Context, headID int64, filter entity.ExpenseFilter) ([]entity.Expense, error)

	UpdateExpense(ctx context.Context, id uuid.UUID, patch entity.ExpensePatch) (entity.Expense, error)

	DeleteExpense(ctx context.Context, id uuid.UUID) error
}

type UserRepo interface {
	UpsertUser(ctx context.Context, user entity.User) error

	GetUser(ctx context.Context, userID int64) (entity.User, error)

	ListUsers(ctx context.Context) ([]entity.User, error)

	ListFamilyUsers(ctx context.Context, headID int64) ([]entity.User, error)

	BecomeHead(ctx context.Context, userID int64, inviteCode string) error

	JoinFamily(ctx context.Context, userID, headID int64) error

	GetHeadByInviteCode(ctx context.Context, code string) (entity.User, error)

	IsUserInFamily(ctx context.Context, headID, userID int64) (bool, error)

	CountFamilyMembers(ctx context.Context, headID int64) (int, error)
}

type LLM interface {
	Chat(ctx context.Context, messages []entity.Message, tools []entity.Tool) (entity.Message, error)
}
