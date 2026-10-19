package user

import (
	"context"
	"database/sql"
	"fmt"

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

func (r *Repo) UpsertUser(ctx context.Context, user entity.User) error {
	const q = `
		INSERT INTO users (user_id, username, full_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE
		SET username = EXCLUDED.username,
		    full_name = EXCLUDED.full_name`
	_, err := r.Pool.Exec(ctx, q, user.UserID, user.Username, user.FullName)
	if err != nil {
		return fmt.Errorf("upsert user: %w", err)
	}
	return nil
}

const userColumns = `user_id, username, full_name, invite_user_id,
	COALESCE(invite_code, '')`

func (r *Repo) GetUser(ctx context.Context, userID int64) (entity.User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE user_id = $1`
	return scanUser(r.Pool.QueryRow(ctx, q, userID))
}

func (r *Repo) ListUsers(ctx context.Context) ([]entity.User, error) {
	q := `SELECT ` + userColumns + ` FROM users ORDER BY full_name, user_id`
	rows, err := r.Pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	users := make([]entity.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *Repo) ListFamilyUsers(ctx context.Context, headID int64) ([]entity.User, error) {
	q := `SELECT ` + userColumns + `
		FROM users
		WHERE user_id = $1 OR invite_user_id = $1
		ORDER BY full_name, user_id`
	rows, err := r.Pool.Query(ctx, q, headID)
	if err != nil {
		return nil, fmt.Errorf("list family users: %w", err)
	}
	defer rows.Close()

	users := make([]entity.User, 0)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (r *Repo) BecomeHead(ctx context.Context, userID int64, inviteCode string) error {
	const q = `
		UPDATE users
		SET invite_user_id = NULL,
		    invite_code = $2
		WHERE user_id = $1`
	tag, err := r.Pool.Exec(ctx, q, userID, inviteCode)
	if err != nil {
		return fmt.Errorf("become head: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return entity.ErrNotFound
	}
	return nil
}

func (r *Repo) JoinFamily(ctx context.Context, userID, headID int64) error {
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
	tag, err := r.Pool.Exec(ctx, q, userID, headID)
	if err != nil {
		return fmt.Errorf("join family: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return entity.ErrNotFound
	}
	return nil
}

func (r *Repo) GetHeadByInviteCode(ctx context.Context, code string) (entity.User, error) {
	if code == "" {
		return entity.User{}, entity.ErrNotFound
	}
	q := `SELECT ` + userColumns + ` FROM users WHERE invite_code = $1 AND invite_user_id IS NULL`
	return scanUser(r.Pool.QueryRow(ctx, q, code))
}

func (r *Repo) IsUserInFamily(ctx context.Context, headID, userID int64) (bool, error) {
	const q = `
		SELECT EXISTS (
		    SELECT 1 FROM users
		    WHERE user_id = $2 AND (user_id = $1 OR invite_user_id = $1)
		)`
	var ok bool
	if err := r.Pool.QueryRow(ctx, q, headID, userID).Scan(&ok); err != nil {
		return false, fmt.Errorf("is user in family: %w", err)
	}
	return ok, nil
}

func (r *Repo) CountFamilyMembers(ctx context.Context, headID int64) (int, error) {
	const q = `SELECT count(*) FROM users WHERE invite_user_id = $1`
	var count int
	if err := r.Pool.QueryRow(ctx, q, headID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count family members: %w", err)
	}
	return count, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(row rowScanner) (entity.User, error) {
	var (
		u      entity.User
		invite sql.NullInt64
	)
	err := row.Scan(&u.UserID, &u.Username, &u.FullName, &invite, &u.InviteCode)
	if err != nil {
		if err == pgx.ErrNoRows {
			return entity.User{}, entity.ErrNotFound
		}
		return entity.User{}, fmt.Errorf("scan user: %w", err)
	}
	if invite.Valid {
		u.InviteUserID = &invite.Int64
	}
	return u, nil
}
