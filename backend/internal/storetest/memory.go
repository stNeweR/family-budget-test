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

// UpsertUser implements store.Store. Family role and credentials of an
// existing user are preserved.
func (m *Memory) UpsertUser(_ context.Context, user domain.User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.users[user.UserID]; ok {
		existing.Username = user.Username
		existing.FullName = user.FullName
		m.users[user.UserID] = existing
		return nil
	}
	m.users[user.UserID] = user
	return nil
}

// GetUser implements store.Store.
func (m *Memory) GetUser(_ context.Context, userID int64) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
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

// ListFamilyUsers implements store.Store.
func (m *Memory) ListFamilyUsers(_ context.Context, headID int64) ([]domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	users := make([]domain.User, 0)
	for _, u := range m.users {
		if u.UserID == headID || (u.InviteUserID != nil && *u.InviteUserID == headID) {
			users = append(users, u)
		}
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].FullName != users[j].FullName {
			return users[i].FullName < users[j].FullName
		}
		return users[i].UserID < users[j].UserID
	})
	return users, nil
}

// BecomeHead implements store.Store.
func (m *Memory) BecomeHead(_ context.Context, userID int64, inviteCode, panelToken string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.InviteUserID = nil
	u.InviteCode = inviteCode
	u.PanelToken = panelToken
	m.users[userID] = u
	return nil
}

// JoinFamily implements store.Store.
func (m *Memory) JoinFamily(_ context.Context, userID, headID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok || userID == headID {
		return domain.ErrNotFound
	}
	head, ok := m.users[headID]
	if !ok || !head.IsHead() {
		return domain.ErrNotFound
	}
	invite := headID
	u.InviteUserID = &invite
	u.InviteCode = ""
	u.PanelToken = ""
	m.users[userID] = u
	return nil
}

// GetHeadByInviteCode implements store.Store.
func (m *Memory) GetHeadByInviteCode(_ context.Context, code string) (domain.User, error) {
	return m.findHead(func(u domain.User) bool { return u.InviteCode == code && code != "" })
}

// GetHeadByPanelToken implements store.Store.
func (m *Memory) GetHeadByPanelToken(_ context.Context, token string) (domain.User, error) {
	return m.findHead(func(u domain.User) bool { return u.PanelToken == token && token != "" })
}

func (m *Memory) findHead(match func(domain.User) bool) (domain.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, u := range m.users {
		if u.IsHead() && match(u) {
			return u, nil
		}
	}
	return domain.User{}, domain.ErrNotFound
}

// IsUserInFamily implements store.Store.
func (m *Memory) IsUserInFamily(_ context.Context, headID, userID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return false, nil
	}
	return u.UserID == headID || (u.InviteUserID != nil && *u.InviteUserID == headID), nil
}

// CountFamilyMembers implements store.Store.
func (m *Memory) CountFamilyMembers(_ context.Context, headID int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	count := 0
	for _, u := range m.users {
		if u.InviteUserID != nil && *u.InviteUserID == headID {
			count++
		}
	}
	return count, nil
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
		all = append(all, e)
	}
	return applyExpenseFilter(all, filter), nil
}

// ListFamilyExpenses implements store.Store.
func (m *Memory) ListFamilyExpenses(_ context.Context, headID int64, filter domain.ExpenseFilter) ([]domain.Expense, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	members := map[int64]bool{headID: true}
	for _, u := range m.users {
		if u.InviteUserID != nil && *u.InviteUserID == headID {
			members[u.UserID] = true
		}
	}

	all := make([]domain.Expense, 0, len(m.expenses))
	for _, e := range m.expenses {
		if members[e.UserID] {
			all = append(all, e)
		}
	}
	return applyExpenseFilter(all, filter), nil
}

// applyExpenseFilter narrows and pages expenses exactly like the Postgres
// implementation.
func applyExpenseFilter(expenses []domain.Expense, filter domain.ExpenseFilter) []domain.Expense {
	all := make([]domain.Expense, 0, len(expenses))
	for _, e := range expenses {
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
			return []domain.Expense{}
		}
		all = all[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(all) {
		all = all[:filter.Limit]
	}
	return all
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
