package expense

import (
	"context"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/repo"
)

type UseCase struct {
	repo repo.ExpenseRepo
}

func New(repo repo.ExpenseRepo) *UseCase {
	return &UseCase{repo: repo}
}

func (s *UseCase) List(ctx context.Context, filter entity.ExpenseFilter) ([]entity.Expense, error) {
	return s.repo.ListExpenses(ctx, filter)
}

func (s *UseCase) ListFamily(ctx context.Context, headID int64, filter entity.ExpenseFilter) ([]entity.Expense, error) {
	return s.repo.ListFamilyExpenses(ctx, headID, filter)
}

func (s *UseCase) Create(ctx context.Context, expense entity.Expense) (entity.Expense, error) {
	if err := s.repo.CreateExpense(ctx, &expense); err != nil {
		return entity.Expense{}, err
	}
	return expense, nil
}

func (s *UseCase) Get(ctx context.Context, id uuid.UUID) (entity.Expense, error) {
	return s.repo.GetExpense(ctx, id)
}

func (s *UseCase) Update(ctx context.Context, id uuid.UUID, patch entity.ExpensePatch) (entity.Expense, error) {
	return s.repo.UpdateExpense(ctx, id, patch)
}

func (s *UseCase) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.DeleteExpense(ctx, id)
}

func (s *UseCase) Owned(ctx context.Context, userID int64, id uuid.UUID) (entity.Expense, error) {
	expense, err := s.repo.GetExpense(ctx, id)
	if err != nil {
		return entity.Expense{}, err
	}
	if expense.UserID != userID {
		return entity.Expense{}, entity.ErrNotFound
	}
	return expense, nil
}

func (s *UseCase) DeleteOwned(ctx context.Context, userID int64, id uuid.UUID) (entity.Expense, error) {
	expense, err := s.Owned(ctx, userID, id)
	if err != nil {
		return entity.Expense{}, err
	}
	if err := s.repo.DeleteExpense(ctx, id); err != nil {
		return entity.Expense{}, err
	}
	return expense, nil
}

func (s *UseCase) Total(ctx context.Context, filter entity.ExpenseFilter) (float64, int, error) {
	expenses, err := s.repo.ListExpenses(ctx, filter)
	if err != nil {
		return 0, 0, err
	}
	var total float64
	for _, e := range expenses {
		total += e.Amount
	}
	return total, len(expenses), nil
}
