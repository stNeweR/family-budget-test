package postgres

import (
	"context"
	"fmt"
)

func (p *Postgres) Reset(ctx context.Context) error {
	const q = `TRUNCATE TABLE expenses, users RESTART IDENTITY CASCADE`
	if _, err := p.Pool.Exec(ctx, q); err != nil {
		return fmt.Errorf("reset tables: %w", err)
	}
	return nil
}
