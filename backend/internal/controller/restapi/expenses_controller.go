package restapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/internal/usecase"
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

type expensesController struct {
	expense usecase.Expense
}

func newExpensesController(expense usecase.Expense) *expensesController {
	return &expensesController{
		expense: expense,
	}
}

func (c *expensesController) Register(r chi.Router) {
	r.Route("/expenses", func(r chi.Router) {
		r.Get("/", c.list)
		r.Post("/", c.create)
		r.Get("/{id}", c.get)
		r.Patch("/{id}", c.update)
		r.Delete("/{id}", c.delete)
	})
}

func (c *expensesController) list(w http.ResponseWriter, r *http.Request) {
	filter, err := parseFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	expenses, err := c.expense.List(r.Context(), filter)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, expenses)
}

func (c *expensesController) create(w http.ResponseWriter, r *http.Request) {
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
	created, err := c.expense.Create(r.Context(), expense)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (c *expensesController) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	expense, err := c.expense.Get(r.Context(), id)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, expense)
}

func (c *expensesController) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
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
	expense, err := c.expense.Update(r.Context(), id, patch)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, expense)
}

func (c *expensesController) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r)
	if !ok {
		return
	}
	if err := c.expense.Delete(r.Context(), id); err != nil {
		writeServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func inputToExpense(in expenseInput) (entity.Expense, error) {
	if in.UserID == 0 {
		return entity.Expense{}, errors.New("user_id is required")
	}
	if in.Amount <= 0 {
		return entity.Expense{}, errors.New("amount must be > 0")
	}
	category := entity.Category(strings.TrimSpace(in.Category))
	if !category.Valid() {
		return entity.Expense{}, errors.New("unknown category")
	}
	spentAt, err := parseDate(in.SpentAt)
	if err != nil {
		return entity.Expense{}, err
	}
	return entity.Expense{
		UserID:   in.UserID,
		Amount:   in.Amount,
		Category: category,
		SpentAt:  spentAt,
		Comment:  strings.TrimSpace(in.Comment),
	}, nil
}

func inputToPatch(in expensePatchInput) (entity.ExpensePatch, error) {
	var patch entity.ExpensePatch
	if in.Amount != nil {
		if *in.Amount <= 0 {
			return patch, errors.New("amount must be > 0")
		}
		patch.Amount = in.Amount
	}
	if in.Category != nil {
		category := entity.Category(strings.TrimSpace(*in.Category))
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

func parseFilter(r *http.Request) (entity.ExpenseFilter, error) {
	q := r.URL.Query()
	filter := entity.ExpenseFilter{
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
		category := entity.Category(raw)
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
