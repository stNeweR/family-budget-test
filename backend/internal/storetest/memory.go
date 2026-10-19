// Package storetest provides an in-memory Store implementation for tests.
package storetest

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/domain"
)

// Memory is a concurrency-safe in-memory store.Store.
type Memory struct {
	mu       sync.Mutex
	users    map[int64]domain.User
	expenses map[uuid.UUID]domain.Expense
	now      func() time.Time
}

// New returns an empty in-memory store.
func New() *Memory {
	return &Memory{
		users:    make(map[int64]domain.User),
		expenses: make(map[uuid.UUID]domain.Expense),
		now:      time.Now,
	}
}

// Ping implements store.Store.
func (m *Memory) Ping(context.Context) error { return nil }

// UpsertUser implements store.Store.
func (m *Memory) UpsertUser(_ context.Context, user domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[user.UserID] = user
	return nil
}

// ListUsers implements store.Store.
func (m *Memory) ListUsers(context.Context) ([]domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	users := make([]domain.User, 0, len(m.users))
	for _, u := range m.users {
		users = append(users, u)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].UserID < users[j].UserID })
	return users, nil
}

// CreateExpense implements store.Store.
func (m *Memory) CreateExpense(_ context.Context, expense *domain.Expense) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if expense.ID == uuid.Nil {
		expense.ID = uuid.New()
	}
	now := m.now()
	expense.CreatedAt = now
	expense.UpdatedAt = now
	m.expenses[expense.ID] = *expense
	return nil
}

// GetExpense implements store.Store.
func (m *Memory) GetExpense(_ context.Context, id uuid.UUID) (domain.Expense, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.expenses[id]
	if !ok {
		return domain.Expense{}, domain.ErrNotFound
	}
	return e, nil
}

// ListExpenses implements store.Store.
func (m *Memory) ListExpenses(_ context.Context, filter domain.ExpenseFilter) ([]domain.Expense, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	all := make([]domain.Expense, 0, len(m.expenses))
	for _, e := range m.expenses {
		if filter.UserID != nil && e.UserID != *filter.UserID {
			continue
		}
		if filter.From != nil && e.SpentAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && e.SpentAt.After(*filter.To) {
			continue
		}
		if filter.Category != nil && e.Category != *filter.Category {
			continue
		}
		all = append(all, e)
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].SpentAt.Equal(all[j].SpentAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].SpentAt.After(all[j].SpentAt)
	})

	if filter.Offset > 0 {
		if filter.Offset >= len(all) {
			return []domain.Expense{}, nil
		}
		all = all[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(all) {
		all = all[:filter.Limit]
	}
	return all, nil
}

// UpdateExpense implements store.Store.
func (m *Memory) UpdateExpense(_ context.Context, id uuid.UUID, patch domain.ExpensePatch) (domain.Expense, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.expenses[id]
	if !ok {
		return domain.Expense{}, domain.ErrNotFound
	}
	if patch.Amount != nil {
		e.Amount = *patch.Amount
	}
	if patch.Category != nil {
		e.Category = *patch.Category
	}
	if patch.SpentAt != nil {
		e.SpentAt = *patch.SpentAt
	}
	if patch.Comment != nil {
		e.Comment = *patch.Comment
	}
	e.UpdatedAt = m.now()
	m.expenses[id] = e
	return e, nil
}

// DeleteExpense implements store.Store.
func (m *Memory) DeleteExpense(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.expenses[id]; !ok {
		return domain.ErrNotFound
	}
	delete(m.expenses, id)
	return nil
}
