package storetest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stNeweR/family-budget/internal/domain"
)

func addUser(t *testing.T, st *Memory, id int64, name string) {
	t.Helper()
	if err := st.UpsertUser(context.Background(), domain.User{UserID: id, FullName: name}); err != nil {
		t.Fatalf("upsert user %d: %v", id, err)
	}
}

// addHead creates a user and promotes them to head with a unique invite code.
func addHead(t *testing.T, st *Memory, id int64) string {
	t.Helper()
	code := fmt.Sprintf("code-%d", id)
	addUser(t, st, id, "Глава")
	if err := st.BecomeHead(context.Background(), id, code); err != nil {
		t.Fatalf("become head %d: %v", id, err)
	}
	return code
}

func TestUpsertUserPreservesFamilyRole(t *testing.T) {
	ctx := context.Background()
	st := New()

	addUser(t, st, 1, "Анна")
	if err := st.BecomeHead(ctx, 1, "code-1"); err != nil {
		t.Fatalf("become head: %v", err)
	}
	addUser(t, st, 2, "Игорь")
	if err := st.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	// Refreshing profiles must not touch roles or credentials.
	if err := st.UpsertUser(ctx, domain.User{UserID: 1, Username: "mama", FullName: "Анна П."}); err != nil {
		t.Fatalf("refresh head: %v", err)
	}
	if err := st.UpsertUser(ctx, domain.User{UserID: 2, Username: "papa", FullName: "Игорь П."}); err != nil {
		t.Fatalf("refresh member: %v", err)
	}

	head, err := st.GetUser(ctx, 1)
	if err != nil {
		t.Fatalf("get head: %v", err)
	}
	if !head.IsHead() || head.InviteCode != "code-1" {
		t.Fatalf("head credentials lost: %+v", head)
	}
	if head.FullName != "Анна П." || head.Username != "mama" {
		t.Fatalf("head profile not refreshed: %+v", head)
	}

	member, err := st.GetUser(ctx, 2)
	if err != nil {
		t.Fatalf("get member: %v", err)
	}
	if member.InviteUserID == nil || *member.InviteUserID != 1 {
		t.Fatalf("member role lost: %+v", member)
	}
	if member.FullName != "Игорь П." {
		t.Fatalf("member profile not refreshed: %+v", member)
	}
}

func TestJoinFamilyClearsHeadCredentials(t *testing.T) {
	ctx := context.Background()
	st := New()
	addHead(t, st, 1)
	oldCode := addHead(t, st, 2)

	if err := st.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	joined, err := st.GetUser(ctx, 2)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if joined.InviteUserID == nil || *joined.InviteUserID != 1 {
		t.Fatalf("invite_user_id not set: %+v", joined)
	}
	if joined.InviteCode != "" {
		t.Fatalf("head credentials not cleared: %+v", joined)
	}

	if _, err := st.GetHeadByInviteCode(ctx, oldCode); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("stale invite code still resolves: %v", err)
	}
}

func TestJoinFamilyRejectsInvalidTargets(t *testing.T) {
	ctx := context.Background()
	st := New()
	addHead(t, st, 1)
	addHead(t, st, 2)
	addUser(t, st, 3, "Участник")
	if err := st.JoinFamily(ctx, 3, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	cases := []struct {
		name           string
		userID, headID int64
	}{
		{"unknown user", 99, 1},
		{"unknown head", 3, 99},
		{"self join", 1, 1},
		{"head is a member", 2, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := st.JoinFamily(ctx, tc.userID, tc.headID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("got %v, want ErrNotFound", err)
			}
		})
	}
}

func TestListFamilyScoping(t *testing.T) {
	ctx := context.Background()
	st := New()
	addHead(t, st, 1)
	addHead(t, st, 5)
	addUser(t, st, 2, "Участник 2")
	addUser(t, st, 3, "Участник 3")
	if err := st.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join 2: %v", err)
	}
	if err := st.JoinFamily(ctx, 3, 1); err != nil {
		t.Fatalf("join 3: %v", err)
	}

	memberIDs := map[int64]bool{1: true, 2: true, 3: true}
	users, err := st.ListFamilyUsers(ctx, 1)
	if err != nil {
		t.Fatalf("list family users: %v", err)
	}
	if len(users) != 3 {
		t.Fatalf("got %d family users, want 3", len(users))
	}
	for _, u := range users {
		if !memberIDs[u.UserID] {
			t.Fatalf("unexpected user in family: %+v", u)
		}
	}

	for _, userID := range []int64{1, 2, 5} {
		spentAt := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
		expense := domain.Expense{UserID: userID, Amount: 100, Category: domain.CategoryFood, SpentAt: spentAt}
		if err := st.CreateExpense(ctx, &expense); err != nil {
			t.Fatalf("create expense: %v", err)
		}
	}

	expenses, err := st.ListFamilyExpenses(ctx, 1, domain.ExpenseFilter{})
	if err != nil {
		t.Fatalf("list family expenses: %v", err)
	}
	if len(expenses) != 2 {
		t.Fatalf("got %d family expenses, want 2", len(expenses))
	}
	for _, e := range expenses {
		if e.UserID == 5 {
			t.Fatalf("outsider expense leaked into family: %+v", e)
		}
	}

	filtered, err := st.ListFamilyExpenses(ctx, 1, domain.ExpenseFilter{UserID: ptr(int64(2))})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].UserID != 2 {
		t.Fatalf("filter by member failed: %+v", filtered)
	}
}

func TestIsUserInFamilyAndCount(t *testing.T) {
	ctx := context.Background()
	st := New()
	addHead(t, st, 1)
	addHead(t, st, 5)
	addUser(t, st, 2, "Участник")
	if err := st.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join 2: %v", err)
	}

	cases := []struct {
		headID, userID int64
		want           bool
	}{
		{1, 1, true},  // head is always in his family
		{1, 2, true},  // member
		{1, 5, false}, // another head
		{1, 99, false},
		{5, 2, false},
	}
	for _, tc := range cases {
		got, err := st.IsUserInFamily(ctx, tc.headID, tc.userID)
		if err != nil {
			t.Fatalf("IsUserInFamily(%d, %d): %v", tc.headID, tc.userID, err)
		}
		if got != tc.want {
			t.Fatalf("IsUserInFamily(%d, %d) = %v, want %v", tc.headID, tc.userID, got, tc.want)
		}
	}

	count, err := st.CountFamilyMembers(ctx, 1)
	if err != nil {
		t.Fatalf("CountFamilyMembers: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d members, want 1", count)
	}
	if count, _ := st.CountFamilyMembers(ctx, 5); count != 0 {
		t.Fatalf("got %d members for outsider head, want 0", count)
	}
}

func TestHeadLookups(t *testing.T) {
	ctx := context.Background()
	st := New()
	code := addHead(t, st, 1)

	head, err := st.GetHeadByInviteCode(ctx, code)
	if err != nil || head.UserID != 1 {
		t.Fatalf("GetHeadByInviteCode: %+v, %v", head, err)
	}

	for _, unknown := range []string{"", "unknown"} {
		if _, err := st.GetHeadByInviteCode(ctx, unknown); !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("code %q: got %v, want ErrNotFound", unknown, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }
