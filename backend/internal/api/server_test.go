package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/domain"
	"github.com/stNeweR/family-budget/internal/storetest"
)

const testToken = "test-panel-token"

// newTestServer builds the router and seeds head user 1 with testToken.
func newTestServer(t *testing.T, st *storetest.Memory) http.Handler {
	t.Helper()
	ctx := t.Context()
	if err := st.UpsertUser(ctx, domain.User{UserID: 1, FullName: "Мама"}); err != nil {
		t.Fatalf("seed head: %v", err)
	}
	if err := st.BecomeHead(ctx, 1, "HEADCODE01", testToken); err != nil {
		t.Fatalf("become head: %v", err)
	}
	log := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	return New(st, log).Handler()
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return doJSONToken(t, h, testToken, method, path, body)
}

func doJSONToken(t *testing.T, h http.Handler, token, method, path string, body any) *httptest.ResponseRecorder {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestListExpensesFilters(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	h := newTestServer(t, st)
	_ = st.UpsertUser(ctx, domain.User{UserID: 2, FullName: "Папа"})
	if err := st.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	base := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	seed := []domain.Expense{
		{UserID: 1, Amount: 100, Category: domain.CategoryFood, SpentAt: base},
		{UserID: 1, Amount: 200, Category: domain.CategoryTransport, SpentAt: base.AddDate(0, 0, 3)},
		{UserID: 2, Amount: 300, Category: domain.CategoryFood, SpentAt: base},
	}
	for i := range seed {
		if err := st.CreateExpense(ctx, &seed[i]); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	rec := doJSON(t, h, http.MethodGet, "/api/expenses?user_id=1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", rec.Code, rec.Body)
	}
	var all []domain.Expense
	if err := json.Unmarshal(rec.Body.Bytes(), &all); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 expenses for user 1, got %d", len(all))
	}

	rec = doJSON(t, h, http.MethodGet, "/api/expenses?user_id=1&category=транспорт", nil)
	var transport []domain.Expense
	_ = json.Unmarshal(rec.Body.Bytes(), &transport)
	if len(transport) != 1 || transport[0].Amount != 200 {
		t.Fatalf("category filter failed: %+v", transport)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/expenses?user_id=1&from=2026-10-12&to=2026-10-13", nil)
	var inRange []domain.Expense
	_ = json.Unmarshal(rec.Body.Bytes(), &inRange)
	if len(inRange) != 1 || inRange[0].Amount != 200 {
		t.Fatalf("date filter failed: %+v", inRange)
	}

	// Invalid category is a 400.
	rec = doJSON(t, h, http.MethodGet, "/api/expenses?category=xyz", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bad category, got %d", rec.Code)
	}
}

func TestCreateUpdateDeleteExpense(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	h := newTestServer(t, st)
	_ = st.UpsertUser(ctx, domain.User{UserID: 7, FullName: "Сын"})
	if err := st.JoinFamily(ctx, 7, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	rec := doJSON(t, h, http.MethodPost, "/api/expenses", expenseInput{
		UserID: 7, Amount: 350, Category: "еда", SpentAt: "2026-10-16", Comment: "кофе",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body)
	}
	var created domain.Expense
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
	var updated domain.Expense
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
	st := storetest.New()
	h := newTestServer(t, st)

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
	st := storetest.New()
	h := newTestServer(t, st)
	rec := doJSON(t, h, http.MethodGet, "/api/expenses/not-a-uuid", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListUsersAndHealth(t *testing.T) {
	st := storetest.New()
	h := newTestServer(t, st)

	rec := doJSON(t, h, http.MethodGet, "/api/users", nil)
	var users []domain.User
	_ = json.Unmarshal(rec.Body.Bytes(), &users)
	if len(users) != 1 || users[0].FullName != "Мама" {
		t.Fatalf("unexpected users: %+v", users)
	}

	rec = doJSON(t, h, http.MethodGet, "/api/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health = %d", rec.Code)
	}
}

func TestAnonymousAccessWithoutToken(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	h := newTestServer(t, st) // head 1 with testToken
	_ = st.UpsertUser(ctx, domain.User{UserID: 5, FullName: "Чужой"})
	expense := domain.Expense{UserID: 5, Amount: 42, Category: domain.CategoryFood, SpentAt: time.Now()}
	if err := st.CreateExpense(ctx, &expense); err != nil {
		t.Fatalf("create expense: %v", err)
	}

	// The task requires an unauthenticated panel: without a token the full
	// view is available.
	rec := doJSONToken(t, h, "", http.MethodGet, "/api/users", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous users: expected 200, got %d", rec.Code)
	}
	var users []domain.User
	_ = json.Unmarshal(rec.Body.Bytes(), &users)
	if len(users) != 2 {
		t.Fatalf("anonymous users = %+v, want 2", users)
	}

	rec = doJSONToken(t, h, "", http.MethodGet, "/api/expenses", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous expenses: expected 200, got %d", rec.Code)
	}
	var list []domain.Expense
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0].ID != expense.ID {
		t.Fatalf("anonymous expenses = %+v", list)
	}

	rec = doJSONToken(t, h, "", http.MethodGet, "/api/expenses/"+expense.ID.String(), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("anonymous get: expected 200, got %d", rec.Code)
	}

	rec = doJSONToken(t, h, "", http.MethodPost, "/api/expenses", expenseInput{UserID: 5, Amount: 1, Category: "еда"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("anonymous create: expected 201, got %d", rec.Code)
	}

	// A stale token must not silently downgrade to the anonymous view.
	rec = doJSONToken(t, h, "wrong-token", http.MethodGet, "/api/users", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", rec.Code)
	}

	// A malformed Authorization header is rejected as well.
	req := httptest.NewRequest(http.MethodGet, "/api/users", nil)
	req.Header.Set("Authorization", "NotBearer xyz")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("malformed header: expected 401, got %d", rec.Code)
	}

	// Health stays public.
	rec = doJSONToken(t, h, "", http.MethodGet, "/api/health", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health without token = %d", rec.Code)
	}
}

func TestFamilyScoping(t *testing.T) {
	ctx := t.Context()
	st := storetest.New()
	h := newTestServer(t, st) // head 1 with testToken

	_ = st.UpsertUser(ctx, domain.User{UserID: 2, FullName: "Папа"})
	if err := st.JoinFamily(ctx, 2, 1); err != nil {
		t.Fatalf("join family: %v", err)
	}

	// A second family that must stay invisible to the first token.
	_ = st.UpsertUser(ctx, domain.User{UserID: 10, FullName: "Чужой"})
	if err := st.BecomeHead(ctx, 10, "OTHERCODE1", "other-token"); err != nil {
		t.Fatalf("become head 10: %v", err)
	}
	_ = st.UpsertUser(ctx, domain.User{UserID: 11, FullName: "Чужой сын"})
	if err := st.JoinFamily(ctx, 11, 10); err != nil {
		t.Fatalf("join family 10: %v", err)
	}

	own := domain.Expense{UserID: 2, Amount: 100, Category: domain.CategoryFood, SpentAt: time.Now()}
	alien := domain.Expense{UserID: 11, Amount: 999, Category: domain.CategoryFood, SpentAt: time.Now()}
	if err := st.CreateExpense(ctx, &own); err != nil {
		t.Fatalf("create own expense: %v", err)
	}
	if err := st.CreateExpense(ctx, &alien); err != nil {
		t.Fatalf("create alien expense: %v", err)
	}

	// List only shows the head's family.
	rec := doJSON(t, h, http.MethodGet, "/api/expenses", nil)
	var list []domain.Expense
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 || list[0].ID != own.ID {
		t.Fatalf("family expenses leak: %+v", list)
	}

	// Filtering by an outsider is rejected.
	rec = doJSON(t, h, http.MethodGet, "/api/expenses?user_id=11", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("filter by outsider: expected 400, got %d", rec.Code)
	}

	// Direct access to an alien expense is a 404.
	alienPath := "/api/expenses/" + alien.ID.String()
	for _, tc := range []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPatch, map[string]float64{"amount": 1}},
		{http.MethodDelete, nil},
	} {
		rec = doJSON(t, h, tc.method, alienPath, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s alien expense: expected 404, got %d", tc.method, rec.Code)
		}
	}

	// Creating an expense for an outsider is forbidden.
	rec = doJSON(t, h, http.MethodPost, "/api/expenses", expenseInput{UserID: 11, Amount: 5, Category: "еда"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create for outsider: expected 403, got %d", rec.Code)
	}

	// User list contains only the head's family.
	rec = doJSON(t, h, http.MethodGet, "/api/users", nil)
	var users []domain.User
	_ = json.Unmarshal(rec.Body.Bytes(), &users)
	if len(users) != 2 {
		t.Fatalf("expected 2 family users, got %+v", users)
	}

	// The other head sees only their own family.
	rec = doJSONToken(t, h, "other-token", http.MethodGet, "/api/expenses", nil)
	list = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode other: %v", err)
	}
	if len(list) != 1 || list[0].ID != alien.ID {
		t.Fatalf("other family expenses: %+v", list)
	}

	// Anonymous requests see everything, like before the token feature.
	rec = doJSONToken(t, h, "", http.MethodGet, "/api/expenses", nil)
	list = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode anonymous: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("anonymous expenses = %d, want 2", len(list))
	}
}

func TestUnknownJSONFieldRejected(t *testing.T) {
	st := storetest.New()
	h := newTestServer(t, st)
	body := fmt.Sprintf(`{"user_id":1,"amount":100,"category":"еда","bogus":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/expenses", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d", rec.Code)
	}
}
