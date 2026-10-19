package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stNeweR/family-budget/internal/domain"
)

// Postgres is a Store backed by PostgreSQL via pgx.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres opens a connection pool and verifies connectivity.
func NewPostgres(ctx context.Context, databaseURL string) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Postgres{pool: pool}, nil
}

// Close releases the underlying pool.
func (p *Postgres) Close() {
	p.pool.Close()
}

// Ping implements Store.
func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// UpsertUser implements Store. Role columns are intentionally not part of the
// insert or the conflict update, so refreshing a profile never resets a family
// role or invite code.
func (p *Postgres) UpsertUser(ctx context.Context, user domain.User) error {
	const q = `
		INSERT INTO users (user_id, username, full_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE
		SET username = EXCLUDED.username,
		    full_name = EXCLUDED.full_name`
	_, err := p.pool.Exec(ctx, q, user.UserID, user.Username, user.FullName)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

const userColumns = `user_id, username, full_name, invite_user_id,
	COALESCE(invite_code, '')`

// GetUser implements Store.
func (p *Postgres) GetUser(ctx context.Context, userID int64) (domain.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE user_id = $1`
	return scanUser(p.pool.QueryRow(ctx, q, userID))
}

// ListUsers implements Store.
func (p *Postgres) ListUsers(ctx context.Context) ([]domain.User, error) {
	q := `SELECT ` + userColumns + ` FROM users ORDER BY full_name, user_id`
	rows, err := p.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// ListFamilyUsers implements Store.
func (p *Postgres) ListFamilyUsers(ctx context.Context, headID int64) ([]domain.User, error) {
	q := `SELECT ` + userColumns + `
		FROM users
		WHERE user_id = $1 OR invite_user_id = $1
		ORDER BY full_name, user_id`
	rows, err := p.pool.Query(ctx, q, headID)
	if err != nil {
		return nil, fmt.Errorf("list family users: %w", err)
	}
	defer rows.Close()

	users := make([]domain.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// BecomeHead implements Store.
func (p *Postgres) BecomeHead(ctx context.Context, userID int64, inviteCode string) error {
	const q = `
		UPDATE users
		SET invite_user_id = NULL,
		    invite_code = $2
		WHERE user_id = $1`
	tag, err := p.pool.Exec(ctx, q, userID, inviteCode)
	if err != nil {
		return fmt.Errorf("become head: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// JoinFamily implements Store.
func (p *Postgres) JoinFamily(ctx context.Context, userID, headID int64) error {
	const q = `
		UPDATE users
		SET invite_user_id = $2,
		    invite_code = NULL
		WHERE user_id = $1
		  AND $1 <> $2
		  AND EXISTS (
		      SELECT 1 FROM users h
		      WHERE h.user_id = $2 AND h.invite_user_id IS NULL
		  )`
	tag, err := p.pool.Exec(ctx, q, userID, headID)
	if err != nil {
		return fmt.Errorf("join family: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// GetHeadByInviteCode implements Store.
func (p *Postgres) GetHeadByInviteCode(ctx context.Context, code string) (domain.User, error) {
	if code == "" {
		return domain.User{}, domain.ErrNotFound
	}
	q := `SELECT ` + userColumns + ` FROM users WHERE invite_code = $1 AND invite_user_id IS NULL`
	return scanUser(p.pool.QueryRow(ctx, q, code))
}

// IsUserInFamily implements Store.
func (p *Postgres) IsUserInFamily(ctx context.Context, headID, userID int64) (bool, error) {
	const q = `
		SELECT EXISTS (
		    SELECT 1 FROM users
		    WHERE user_id = $2 AND (user_id = $1 OR invite_user_id = $1)
		)`
	var ok bool
	if err := p.pool.QueryRow(ctx, q, headID, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("is user in family: %w", err)
	}
	return ok, nil
}

// CountFamilyMembers implements Store.
func (p *Postgres) CountFamilyMembers(ctx context.Context, headID int64) (int, error) {
	const q = `SELECT count(*) FROM users WHERE invite_user_id = $1`
	var count int
	if err := p.pool.QueryRow(ctx, q, headID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count family members: %w", err)
	}
	return count, nil
}

// CreateExpense implements Store.
func (p *Postgres) CreateExpense(ctx context.Context, expense *domain.Expense) error {
	const q = `
		INSERT INTO expenses (user_id, amount, category, spent_at, comment)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at, updated_at`
	err := p.pool.QueryRow(ctx, q,
		expense.UserID, expense.Amount, string(expense.Category), expense.SpentAt, expense.Comment,
	).Scan(&expense.ID, &expense.CreatedAt, &expense.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create expense: %w", err)
	}
	return nil
}

const expenseColumns = `id, user_id, amount, category, spent_at, comment, created_at, updated_at`

const expenseColumnsPrefixed = `e.id, e.user_id, e.amount, e.category, e.spent_at, e.comment, e.created_at, e.updated_at`

// GetExpense implements Store.
func (p *Postgres) GetExpense(ctx context.Context, id uuid.UUID) (domain.Expense, error) {
	q := `SELECT ` + expenseColumns + ` FROM expenses WHERE id = $1`
	row := p.pool.QueryRow(ctx, q, id)
	return scanExpense(row)
}

// appendExpenseConds appends WHERE conditions for the filter, numbering
// placeholders after the already collected args. Prefix is prepended to column
// names, e.g. "e." for a joined query.
func appendExpenseConds(conds []string, args []any, filter domain.ExpenseFilter, prefix string) ([]string, []any) {
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

// expensePageTail appends ORDER BY, LIMIT and OFFSET with placeholder numbers
// continuing after len(args)-1 args already used. It returns the final args.
func expensePageTail(sb *strings.Builder, args []any, orderBy string, filter domain.ExpenseFilter) []any {
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

// ListExpenses implements Store.
func (p *Postgres) ListExpenses(ctx context.Context, filter domain.ExpenseFilter) ([]domain.Expense, error) {
	conds, args := appendExpenseConds(nil, nil, filter, "")

	var sb strings.Builder
	sb.WriteString(`SELECT ` + expenseColumns + ` FROM expenses`)
	if len(conds) > 0 {
		sb.WriteString(" WHERE " + strings.Join(conds, " AND "))
	}
	args = expensePageTail(&sb, args, "spent_at DESC, created_at DESC", filter)

	rows, err := p.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list expenses: %w", err)
	}
	defer rows.Close()

	expenses := make([]domain.Expense, 0)
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		expenses = append(expenses, e)
	}
	return expenses, rows.Err()
}

// ListFamilyExpenses implements Store.
func (p *Postgres) ListFamilyExpenses(ctx context.Context, headID int64, filter domain.ExpenseFilter) ([]domain.Expense, error) {
	conds := []string{"(e.user_id = $1 OR u.invite_user_id = $1)"}
	args := []any{headID}
	conds, args = appendExpenseConds(conds, args, filter, "e.")

	var sb strings.Builder
	sb.WriteString(`SELECT ` + expenseColumnsPrefixed + `
		FROM expenses e
		JOIN users u ON u.user_id = e.user_id
		WHERE ` + strings.Join(conds, " AND "))
	args = expensePageTail(&sb, args, "e.spent_at DESC, e.created_at DESC", filter)

	rows, err := p.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("list family expenses: %w", err)
	}
	defer rows.Close()

	expenses := make([]domain.Expense, 0)
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		expenses = append(expenses, e)
	}
	return expenses, rows.Err()
}

// UpdateExpense implements Store.
func (p *Postgres) UpdateExpense(ctx context.Context, id uuid.UUID, patch domain.ExpensePatch) (domain.Expense, error) {
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
		return p.GetExpense(ctx, id)
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)

	q := fmt.Sprintf(
		`UPDATE expenses SET %s WHERE id = $%d RETURNING %s`,
		strings.Join(sets, ", "), len(args), expenseColumns,
	)
	return scanExpense(p.pool.QueryRow(ctx, q, args...))
}

// DeleteExpense implements Store.
func (p *Postgres) DeleteExpense(ctx context.Context, id uuid.UUID) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM expenses WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete expense: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (domain.User, error) {
	var (
		u      domain.User
		invite sql.NullInt64
	)
	err := row.Scan(&u.UserID, &u.Username, &u.FullName, &invite, &u.InviteCode)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.User{}, domain.ErrNotFound
		}
		return domain.User{}, fmt.Errorf("scan user: %w", err)
	}
	if invite.Valid {
		u.InviteUserID = &invite.Int64
	}
	return u, nil
}

func scanExpense(row rowScanner) (domain.Expense, error) {
	var (
		e        domain.Expense
		category string
	)
	err := row.Scan(
		&e.ID, &e.UserID, &e.Amount, &category, &e.SpentAt, &e.Comment, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.Expense{}, domain.ErrNotFound
		}
		return domain.Expense{}, fmt.Errorf("scan expense: %w", err)
	}
	e.Category = domain.Category(category)
	return e, nil
}
