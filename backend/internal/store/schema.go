package store

import (
	"context"
	"fmt"
)

// CheckSchema verifies that the migrations have been applied. It returns a
// descriptive error telling the operator to run the goose migrations when the
// expected tables are missing.
func (p *Postgres) CheckSchema(ctx context.Context) error {
	const q = `
		SELECT to_regclass('public.expenses') IS NOT NULL
		   AND to_regclass('public.users') IS NOT NULL`
	var ready bool
	if err := p.pool.QueryRow(ctx, q).Scan(&ready); err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !ready {
		return fmt.Errorf("database schema is not migrated: run `make migrate-up`")
	}
	return nil
}
