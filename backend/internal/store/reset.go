package store

import (
	"context"
	"fmt"
)

// Reset wipes all expenses and users. It is intentionally not part of the
// Store interface because only the seed command needs it.
func (p *Postgres) Reset(ctx context.Context) error {
	const q = `TRUNCATE TABLE expenses, users RESTART IDENTITY CASCADE`
	if _, err := p.pool.Exec(ctx, q); err != nil {
		return fmt.Errorf("reset tables: %w", err)
	}
	return nil
}
