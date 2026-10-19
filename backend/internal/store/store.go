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

	// UpsertUser creates a user or refreshes their profile. Family role,
	// panel token and invite code are left untouched.
	UpsertUser(ctx context.Context, user domain.User) error
	// GetUser returns a single user or domain.ErrNotFound.
	GetUser(ctx context.Context, userID int64) (domain.User, error)
	// ListUsers returns all known users ordered by name.
	ListUsers(ctx context.Context) ([]domain.User, error)
	// ListFamilyUsers returns the head and their members ordered by name.
	ListFamilyUsers(ctx context.Context, headID int64) ([]domain.User, error)

	// BecomeHead promotes the user to family head with fresh credentials.
	BecomeHead(ctx context.Context, userID int64, inviteCode, panelToken string) error
	// JoinFamily attaches the user to the head's family, dropping any head
	// credentials. It returns domain.ErrNotFound when the user or the head
	// does not exist, when the head is not a head, or on self-join.
	JoinFamily(ctx context.Context, userID, headID int64) error
	// GetHeadByInviteCode returns the head owning the invite code.
	GetHeadByInviteCode(ctx context.Context, code string) (domain.User, error)
	// GetHeadByPanelToken returns the head owning the panel token.
	GetHeadByPanelToken(ctx context.Context, token string) (domain.User, error)
	// IsUserInFamily reports whether the user belongs to the head's family.
	IsUserInFamily(ctx context.Context, headID, userID int64) (bool, error)
	// CountFamilyMembers returns the number of members (excluding the head).
	CountFamilyMembers(ctx context.Context, headID int64) (int, error)

	// CreateExpense inserts a new expense and fills ID/created/updated fields.
	CreateExpense(ctx context.Context, expense *domain.Expense) error
	// GetExpense returns a single expense or domain.ErrNotFound.
	GetExpense(ctx context.Context, id uuid.UUID) (domain.Expense, error)
	// ListExpenses returns expenses matching the filter ordered by spent_at desc.
	ListExpenses(ctx context.Context, filter domain.ExpenseFilter) ([]domain.Expense, error)
	// ListFamilyExpenses returns the expenses of the head and their members.
	ListFamilyExpenses(ctx context.Context, headID int64, filter domain.ExpenseFilter) ([]domain.Expense, error)
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
