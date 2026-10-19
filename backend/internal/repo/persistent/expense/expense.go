package expense

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/stNeweR/family-budget/internal/entity"
	"github.com/stNeweR/family-budget/pkg/postgres"
)

type Repo struct {
	*postgres.Postgres
}

func New(pg *postgres.Postgres) *Repo {
	return &Repo{Postgres: pg}
}

func (r *Repo) CreateExpense(ctx context.Context, expense *entity.Expense) error {
	const q = `
		INSERT INTO expenses (user_id, amount, category, spent_at, comment)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`
	err := r.Pool.QueryRow(ctx, q,
		expense.UserID, expense.Amount, string(expense.Category), expense.SpentAt, expense.Comment,
	).Scan(&expense.ID, &expense.CreatedAt, &expense.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create expense: %w", err)
	}
	return nil
}

const expenseColumns = `id, user_id, amount, category, spent_at, comment, created_at, updated_at`

const expenseColumnsPrefixed = `e.id, e.user_id, e.amount, e.category, e.spent_at, e.comment, e.created_at, e.updated_at`

func (r *Repo) GetExpense(ctx context.Context, id uuid.UUID) (entity.Expense, error) {
	q := `SELECT ` + expenseColumns + ` FROM expenses WHERE id = $1`
	row := r.Pool.QueryRow(ctx, q, id)
	return scanExpense(row)
}

func appendExpenseConds(conds []string, args []any, filter entity.ExpenseFilter, prefix string) ([]string, []any) {
	add := func(cond string, value any) {
		args = append(args, value)
		conds = append(conds, fmt.Sprintf(prefix+cond, len(args)))
	}
	if filter.UserID != nil {
		add("user_id = $%d", *filter.UserID)
	}
	if filter.From != nil {
		add("spent_at >= $%d", *filter.From)
	}
	if filter.To != nil {
		add("spent_at <= $%d", *filter.To)
	}
	if filter.Category != nil {
		add("category = $%d", string(*filter.Category))
	}
	return conds, args
}

func expensePageTail(sb *strings.Builder, args []any, orderBy string, filter entity.ExpenseFilter) []any {
	sb.WriteString(" ORDER BY " + orderBy)

	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	args = append(args, limit)
	fmt.Fprintf(sb, " LIMIT $%d", len(args))
	if filter.Offset > 0 {
		args = append(args, filter.Offset)
		fmt.Fprintf(sb, " OFFSET $%d", len(args))
	}
	return args
}

func (r *Repo) ListExpenses(ctx context.Context, filter entity.ExpenseFilter) ([]entity.Expense, error) {
	conds, args := appendExpenseConds(nil, nil, filter, "")

	var sb strings.Builder
	sb.WriteString(`SELECT ` + expenseColumns + ` FROM expenses`)
	if len(conds) > 0 {
		sb.WriteString(" WHERE " + strings.Join(conds, " AND "))
	}
	args = expensePageTail(&sb, args, "spent_at DESC, created_at DESC", filter)

	rows, err := r.Pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list expenses: %w", err)
	}
	defer rows.Close()

	expenses := make([]entity.Expense, 0)
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		expenses = append(expenses, e)
	}
	return expenses, rows.Err()
}

func (r *Repo) ListFamilyExpenses(ctx context.Context, headID int64, filter entity.ExpenseFilter) ([]entity.Expense, error) {
	conds := []string{"(e.user_id = $1 OR u.invite_user_id = $1)"}
	args := []any{headID}
	conds, args = appendExpenseConds(conds, args, filter, "e.")

	var sb strings.Builder
	sb.WriteString(`SELECT ` + expenseColumnsPrefixed + `
		FROM expenses e
		JOIN users u ON u.user_id = e.user_id
		WHERE ` + strings.Join(conds, " AND "))
	args = expensePageTail(&sb, args, "e.spent_at DESC, e.created_at DESC", filter)

	rows, err := r.Pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list family expenses: %w", err)
	}
	defer rows.Close()

	expenses := make([]entity.Expense, 0)
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		expenses = append(expenses, e)
	}
	return expenses, rows.Err()
}

func (r *Repo) UpdateExpense(ctx context.Context, id uuid.UUID, patch entity.ExpensePatch) (entity.Expense, error) {
	var (
		sets []string
		args []any
	)
	add := func(col string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if patch.Amount != nil {
		add("amount", *patch.Amount)
	}
	if patch.Category != nil {
		add("category", string(*patch.Category))
	}
	if patch.SpentAt != nil {
		add("spent_at", *patch.SpentAt)
	}
	if patch.Comment != nil {
		add("comment", *patch.Comment)
	}
	if len(sets) == 0 {
		return r.GetExpense(ctx, id)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)

	q := fmt.Sprintf(
		`UPDATE expenses SET %s WHERE id = $%d RETURNING %s`,
		strings.Join(sets, ", "), len(args), expenseColumns,
	)
	return scanExpense(r.Pool.QueryRow(ctx, q, args...))
}

func (r *Repo) DeleteExpense(ctx context.Context, id uuid.UUID) error {
	tag, err := r.Pool.Exec(ctx, `DELETE FROM expenses WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete expense: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return entity.ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExpense(row rowScanner) (entity.Expense, error) {
	var (
		e        entity.Expense
		category string
	)
	err := row.Scan(
		&e.ID, &e.UserID, &e.Amount, &category, &e.SpentAt, &e.Comment, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return entity.Expense{}, entity.ErrNotFound
		}
		return entity.Expense{}, fmt.Errorf("scan expense: %w", err)
	}
	e.Category = entity.Category(category)
	return e, nil
}
