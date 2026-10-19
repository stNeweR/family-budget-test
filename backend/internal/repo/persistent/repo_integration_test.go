package persistent_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/testsupport"
)

func TestPostgresFamilyFlow(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	users := env.Users
	expensesRepo := env.Expenses

	mustUpsert := func(u entity.User) {
		t.Helper()
		if err := users.UpsertUser(ctx, u); err != nil {
			t.Fatalf("upsert %d: %v", u.UserID, err)
		}
	}

	mustUpsert(entity.User{UserID: 1, Username: "mama", FullName: "Анна"})
	mustUpsert(entity.User{UserID: 2, Username: "papa", FullName: "Игорь"})
	mustUpsert(entity.User{UserID: 5, Username: "other", FullName: "Чужой"})

	if err := users.BecomeHead(ctx, 1, "HEADCODE01"); err != nil {
		t.Fatalf("become head: %v", err)
	}
	if err := users.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	mustUpsert(entity.User{UserID: 1, Username: "mama2", FullName: "Анна П."})
	mustUpsert(entity.User{UserID: 2, Username: "papa2", FullName: "Игорь П."})

	head, err := users.GetUser(ctx, 1)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !head.IsHead() || head.InviteCode != "HEADCODE01" {
		t.Fatalf("head credentials lost: %+v", head)
	}
	if head.FullName != "Анна П." {
		t.Fatalf("head profile not refreshed: %+v", head)
	}

	byCode, err := users.GetHeadByInviteCode(ctx, "HEADCODE01")
	if err != nil || byCode.UserID != 1 {
		t.Fatalf("head by code: %+v, %v", byCode, err)
	}
	if _, err := users.GetHeadByInviteCode(ctx, "unknown"); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("unknown code: got %v, want ErrNotFound", err)
	}

	family, err := users.ListFamilyUsers(ctx, 1)
	if err != nil || len(family) != 2 {
		t.Fatalf("family users: %+v, %v", family, err)
	}
	if count, _ := users.CountFamilyMembers(ctx, 1); count != 1 {
		t.Fatalf("members count = %d, want 1", count)
	}
	if ok, _ := users.IsUserInFamily(ctx, 1, 2); !ok {
		t.Fatal("member must be in family")
	}
	if ok, _ := users.IsUserInFamily(ctx, 1, 5); ok {
		t.Fatal("outsider must not be in family")
	}

	spentAt := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	for _, userID := range []int64{1, 2, 5} {
		e := entity.Expense{UserID: userID, Amount: 100, Category: entity.CategoryFood, SpentAt: spentAt}
		if err := expensesRepo.CreateExpense(ctx, &e); err != nil {
			t.Fatalf("create expense: %v", err)
		}
	}
	expenses, err := expensesRepo.ListFamilyExpenses(ctx, 1, entity.ExpenseFilter{})
	if err != nil || len(expenses) != 2 {
		t.Fatalf("family expenses: %+v, %v", expenses, err)
	}
	memberID := int64(2)
	filtered, err := expensesRepo.ListFamilyExpenses(ctx, 1, entity.ExpenseFilter{UserID: &memberID})
	if err != nil || len(filtered) != 1 || filtered[0].UserID != 2 {
		t.Fatalf("filtered expenses: %+v, %v", filtered, err)
	}

	if err := users.BecomeHead(ctx, 5, "HEADCODE05"); err != nil {
		t.Fatalf("become head 5: %v", err)
	}
	if err := users.JoinFamily(ctx, 2, 5); err != nil {
		t.Fatalf("move member: %v", err)
	}
	moved, err := users.GetUser(ctx, 2)
	if err != nil {
		t.Fatalf("get moved member: %v", err)
	}
	if moved.InviteUserID == nil || *moved.InviteUserID != 5 {
		t.Fatalf("member not moved: %+v", moved)
	}
	if count, _ := users.CountFamilyMembers(ctx, 1); count != 0 {
		t.Fatalf("old family still has members: %d", count)
	}
	if err := users.JoinFamily(ctx, 1, 1); !errors.Is(err, entity.ErrNotFound) {
		t.Fatalf("self join: got %v, want ErrNotFound", err)
	}
}
