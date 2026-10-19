package restapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/testsupport"
	expenseuc "github.com/stNeweR/family-budget/internal/usecase/expense"
	usersuc "github.com/stNeweR/family-budget/internal/usecase/users"
)

func newTestServer(t *testing.T, env testsupport.Env) http.Handler {
	t.Helper()
	return New(
		expenseuc.New(env.Expenses),
		usersuc.New(env.Users),
		env.DB,
	).Handler()
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListExpensesFilters(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	if err := env.Users.UpsertUser(ctx, entity.User{UserID: 1, FullName: "Мама"}); err != nil {
		t.Fatalf("seed user 1: %v", err)
	}
	if err := env.Users.UpsertUser(ctx, entity.User{UserID: 2, FullName: "Папа"}); err != nil {
		t.Fatalf("seed user 2: %v", err)
	}

	base := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	seed := []entity.Expense{
		{UserID: 1, Amount: 100, Category: entity.CategoryFood, SpentAt: base},
		{UserID: 1, Amount: 200, Category: entity.CategoryTransport, SpentAt: base.AddDate(0, 0, 3)},
		{UserID: 2, Amount: 300, Category: entity.CategoryFood, SpentAt: base},
	}
	for i := range seed {
		if err := env.Expenses.CreateExpense(ctx, &seed[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	h := newTestServer(t, env)

	rec := doJSON(t, h, http.MethodGet, "/api/expenses?user_id=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body)
	}
	var all []entity.Expense
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 expenses for user 1, got %d", len(all))
	}

	rec = doJSON(t, h, http.MethodGet, "/api/expenses?user_id=1&category=транспорт", nil)
	var transport []entity.Expense
	_ = json.Unmarshal(rec.Body.Bytes(), &transport)
	if len(transport) != 1 || transport[0].Amount != 200 {
		t.Fatalf("category filter failed: %+v", transport)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/expenses?user_id=1&from=2026-10-12&to=2026-10-13", nil)
	var inRange []entity.Expense
	_ = json.Unmarshal(rec.Body.Bytes(), &inRange)
	if len(inRange) != 1 || inRange[0].Amount != 200 {
		t.Fatalf("date filter failed: %+v", inRange)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/expenses?category=xyz", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad category, got %d", rec.Code)
	}
}

func TestCreateUpdateDeleteExpense(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	if err := env.Users.UpsertUser(ctx, entity.User{UserID: 7, FullName: "Старый"}); err != nil {
		t.Fatalf("seed user 7: %v", err)
	}
	h := newTestServer(t, env)

	rec := doJSON(t, h, http.MethodPost, "/api/expenses", expenseInput{
		UserID: 7, Amount: 350, Category: "еда", SpentAt: "2026-10-16", Comment: "кофе",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body)
	}
	var created entity.Expense
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("expected id to be assigned")
	}

	rec = doJSON(t, h, http.MethodPatch, "/api/expenses/"+created.ID.String(), map[string]float64{"amount": 450})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body)
	}
	var updated entity.Expense
	_ = json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Amount != 450 {
		t.Fatalf("patch did not update amount: %+v", updated)
	}

	rec = doJSON(t, h, http.MethodDelete, "/api/expenses/"+created.ID.String(), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/expenses/"+created.ID.String(), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestCreateExpenseValidation(t *testing.T) {
	env := testsupport.NewEnv(t)
	h := newTestServer(t, env)

	cases := []struct {
		name string
		body expenseInput
	}{
		{"missing user", expenseInput{Amount: 100, Category: "еда"}},
		{"non positive amount", expenseInput{UserID: 1, Amount: 0, Category: "еда"}},
		{"unknown category", expenseInput{UserID: 1, Amount: 100, Category: "bitcoin"}},
		{"bad date", expenseInput{UserID: 1, Amount: 100, Category: "еда", SpentAt: "16.10.2026"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, h, http.MethodPost, "/api/expenses", tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body)
			}
		})
	}
}

func TestGetExpenseInvalidUUID(t *testing.T) {
	env := testsupport.NewEnv(t)
	h := newTestServer(t, env)
	rec := doJSON(t, h, http.MethodGet, "/api/expenses/not-a-uuid", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListUsersAndHealth(t *testing.T) {
	ctx := t.Context()
	env := testsupport.NewEnv(t)
	if err := env.Users.UpsertUser(ctx, entity.User{UserID: 1, FullName: "Мама"}); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	h := newTestServer(t, env)

	rec := doJSON(t, h, http.MethodGet, "/api/users", nil)
	var users []entity.User
	_ = json.Unmarshal(rec.Body.Bytes(), &users)
	if len(users) != 1 || users[0].FullName != "Мама" {
		t.Fatalf("unexpected users: %+v", users)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d", rec.Code)
	}
}

func TestUnknownJSONFieldRejected(t *testing.T) {
	env := testsupport.NewEnv(t)
	h := newTestServer(t, env)
	body := fmt.Sprintf(`{"user_id":1,"amount":100,"category":"еда","bogus":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/expenses", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", rec.Code)
	}
}
