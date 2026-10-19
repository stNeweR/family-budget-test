package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stNeweR/family-budget/internal/domain"
)

// newTestPostgres connects to TEST_DATABASE_URL, which must point to a
// throwaway database with the migrations applied; the test is skipped when the
// variable is not set.
func newTestPostgres(t *testing.T) *Postgres {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}
	ctx := context.Background()
	st, err := NewPostgres(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(st.Close)
	if err := st.CheckSchema(ctx); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if err := st.Reset(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	return st
}

func TestPostgresFamilyFlow(t *testing.T) {
	ctx := t.Context()
	st := newTestPostgres(t)

	mustUpsert := func(u domain.User) {
		t.Helper()
		if err := st.UpsertUser(ctx, u); err != nil {
			t.Fatalf("upsert %d: %v", u.UserID, err)
		}
	}

	mustUpsert(domain.User{UserID: 1, Username: "mama", FullName: "Анна"})
	mustUpsert(domain.User{UserID: 2, Username: "papa", FullName: "Игорь"})
	mustUpsert(domain.User{UserID: 5, Username: "other", FullName: "Чужой"})

	if err := st.BecomeHead(ctx, 1, "HEADCODE01"); err != nil {
		t.Fatalf("become head: %v", err)
	}
	if err := st.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	// Profile refresh must keep the role and credentials.
	mustUpsert(domain.User{UserID: 1, Username: "mama2", FullName: "Анна П."})
	mustUpsert(domain.User{UserID: 2, Username: "papa2", FullName: "Игорь П."})

	head, err := st.GetUser(ctx, 1)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !head.IsHead() || head.InviteCode != "HEADCODE01" {
		t.Fatalf("head credentials lost: %+v", head)
	}
	if head.FullName != "Анна П." {
		t.Fatalf("head profile not refreshed: %+v", head)
	}

	byCode, err := st.GetHeadByInviteCode(ctx, "HEADCODE01")
	if err != nil || byCode.UserID != 1 {
		t.Fatalf("head by code: %+v, %v", byCode, err)
	}
	if _, err := st.GetHeadByInviteCode(ctx, "unknown"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unknown code: got %v, want ErrNotFound", err)
	}

	family, err := st.ListFamilyUsers(ctx, 1)
	if err != nil || len(family) != 2 {
		t.Fatalf("family users: %+v, %v", family, err)
	}
	if count, _ := st.CountFamilyMembers(ctx, 1); count != 1 {
		t.Fatalf("members count = %d, want 1", count)
	}
	if ok, _ := st.IsUserInFamily(ctx, 1, 2); !ok {
		t.Fatal("member must be in family")
	}
	if ok, _ := st.IsUserInFamily(ctx, 1, 5); ok {
		t.Fatal("outsider must not be in family")
	}

	spentAt := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for _, userID := range []int64{1, 2, 5} {
		e := domain.Expense{UserID: userID, Amount: 100, Category: domain.CategoryFood, SpentAt: spentAt}
		if err := st.CreateExpense(ctx, &e); err != nil {
			t.Fatalf("create expense: %v", err)
		}
	}
	expenses, err := st.ListFamilyExpenses(ctx, 1, domain.ExpenseFilter{})
	if err != nil || len(expenses) != 2 {
		t.Fatalf("family expenses: %+v, %v", expenses, err)
	}
	memberID := int64(2)
	filtered, err := st.ListFamilyExpenses(ctx, 1, domain.ExpenseFilter{UserID: &memberID})
	if err != nil || len(filtered) != 1 || filtered[0].UserID != 2 {
		t.Fatalf("filtered expenses: %+v, %v", filtered, err)
	}

	// The member moves to another family; stale lookups must stop working.
	if err := st.BecomeHead(ctx, 5, "HEADCODE05"); err != nil {
		t.Fatalf("become head 5: %v", err)
	}
	if err := st.JoinFamily(ctx, 2, 5); err != nil {
		t.Fatalf("move member: %v", err)
	}
	moved, err := st.GetUser(ctx, 2)
	if err != nil {
		t.Fatalf("get moved member: %v", err)
	}
	if moved.InviteUserID == nil || *moved.InviteUserID != 5 {
		t.Fatalf("member not moved: %+v", moved)
	}
	if count, _ := st.CountFamilyMembers(ctx, 1); count != 0 {
		t.Fatalf("old family still has members: %d", count)
	}
	if err := st.JoinFamily(ctx, 1, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("self join: got %v, want ErrNotFound", err)
	}
}
