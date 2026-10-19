package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/domain"
)

const dateLayout = "2006-01-02"

type expenseInput struct {
	UserID   int64   `json:"user_id"`
	Amount   float64 `json:"amount"`
	Category string  `json:"category"`
	SpentAt  string  `json:"spent_at"`
	Comment  string  `json:"comment"`
}

type expensePatchInput struct {
	Amount   *float64 `json:"amount"`
	Category *string  `json:"category"`
	SpentAt  *string  `json:"spent_at"`
	Comment  *string  `json:"comment"`
}

func (s *Server) listExpenses(w http.ResponseWriter, r *http.Request) {
	head, scoped := headFrom(r.Context())
	filter, err := parseFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if scoped {
		if filter.UserID != nil && *filter.UserID != head.UserID {
			ok, err := s.canAccessExpense(r.Context(), head, *filter.UserID)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			if !ok {
				writeError(w, http.StatusBadRequest, "user_id is not in your family")
				return
			}
		}
		expenses, err := s.store.ListFamilyExpenses(r.Context(), head.UserID, filter)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, expenses)
		return
	}
	expenses, err := s.store.ListExpenses(r.Context(), filter)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, expenses)
}

func (s *Server) createExpense(w http.ResponseWriter, r *http.Request) {
	head, scoped := headFrom(r.Context())
	var in expenseInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	expense, err := inputToExpense(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if scoped {
		ok, err := s.canAccessExpense(r.Context(), head, expense.UserID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if !ok {
			writeError(w, http.StatusForbidden, "user_id is not in your family")
			return
		}
	}
	if err := s.store.CreateExpense(r.Context(), &expense); err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, expense)
}

func (s *Server) getExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	expense, ok := s.loadAccessibleExpense(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, expense)
}

func (s *Server) updateExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if _, ok := s.loadAccessibleExpense(w, r, id); !ok {
		return
	}
	var in expensePatchInput
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	patch, err := inputToPatch(in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	expense, err := s.store.UpdateExpense(r.Context(), id, patch)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, expense)
}

func (s *Server) deleteExpense(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if _, ok := s.loadAccessibleExpense(w, r, id); !ok {
		return
	}
	if err := s.store.DeleteExpense(r.Context(), id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	head, scoped := headFrom(r.Context())
	var (
		users []domain.User
		err   error
	)
	if scoped {
		users, err = s.store.ListFamilyUsers(r.Context(), head.UserID)
	} else {
		users, err = s.store.ListUsers(r.Context())
	}
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func inputToExpense(in expenseInput) (domain.Expense, error) {
	if in.UserID == 0 {
		return domain.Expense{}, errors.New("user_id is required")
	}
	if in.Amount <= 0 {
		return domain.Expense{}, errors.New("amount must be > 0")
	}
	category := domain.Category(strings.TrimSpace(in.Category))
	if !category.Valid() {
		return domain.Expense{}, errors.New("unknown category")
	}
	spentAt, err := parseDate(in.SpentAt)
	if err != nil {
		return domain.Expense{}, err
	}
	return domain.Expense{
		UserID:   in.UserID,
		Amount:   in.Amount,
		Category: category,
		SpentAt:  spentAt,
		Comment:  strings.TrimSpace(in.Comment),
	}, nil
}

func inputToPatch(in expensePatchInput) (domain.ExpensePatch, error) {
	var patch domain.ExpensePatch
	if in.Amount != nil {
		if *in.Amount <= 0 {
			return patch, errors.New("amount must be > 0")
		}
		patch.Amount = in.Amount
	}
	if in.Category != nil {
		category := domain.Category(strings.TrimSpace(*in.Category))
		if !category.Valid() {
			return patch, errors.New("unknown category")
		}
		patch.Category = &category
	}
	if in.SpentAt != nil {
		spentAt, err := parseDate(*in.SpentAt)
		if err != nil {
			return patch, err
		}
		patch.SpentAt = &spentAt
	}
	if in.Comment != nil {
		comment := strings.TrimSpace(*in.Comment)
		patch.Comment = &comment
	}
	return patch, nil
}

func parseFilter(r *http.Request) (domain.ExpenseFilter, error) {
	q := r.URL.Query()
	filter := domain.ExpenseFilter{
		Limit:  parseIntDefault(q.Get("limit"), 100),
		Offset: parseIntDefault(q.Get("offset"), 0),
	}
	if raw := q.Get("user_id"); raw != "" {
		uid, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return filter, errors.New("user_id must be an integer")
		}
		filter.UserID = &uid
	}
	if raw := q.Get("from"); raw != "" {
		d, err := parseDate(raw)
		if err != nil {
			return filter, err
		}
		filter.From = &d
	}
	if raw := q.Get("to"); raw != "" {
		d, err := parseDate(raw)
		if err != nil {
			return filter, err
		}
		filter.To = &d
	}
	if raw := q.Get("category"); raw != "" {
		category := domain.Category(raw)
		if !category.Valid() {
			return filter, errors.New("unknown category")
		}
		filter.Category = &category
	}
	return filter, nil
}

func parseDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC().Truncate(24 * time.Hour), nil
	}
	d, err := time.Parse(dateLayout, value)
	if err != nil {
		return time.Time{}, errors.New("date must be in YYYY-MM-DD format")
	}
	return d, nil
}

func parseIntDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return n
}

func pathUUID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid expense id")
		return uuid.Nil, false
	}
	return id, true
}
