package postgres

import (
	"context"
	"fmt"
)

func (p *Postgres) CheckSchema(ctx context.Context) error {
	const q = `
		SELECT to_regclass('public.expenses') IS NOT NULL
		   AND to_regclass('public.users') IS NOT NULL
		   AND EXISTS (
		       SELECT 1 FROM information_schema.columns
		       WHERE table_schema = 'public'
		         AND table_name = 'users'
		         AND column_name = 'invite_user_id'
		   )`
	var ready bool
	if err := p.Pool.QueryRow(ctx, q).Scan(&ready); err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !ready {
		return fmt.Errorf("database schema is not migrated: run `make migrate-up`")
	}
	return nil
}
