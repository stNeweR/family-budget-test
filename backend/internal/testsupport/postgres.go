package testsupport

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	expenserepo "github.com/stNeweR/family-budget/internal/repo/persistent/expense"
	userrepo "github.com/stNeweR/family-budget/internal/repo/persistent/user"
	"github.com/stNeweR/family-budget/pkg/postgres"
)

type Env struct {
	DB       *postgres.Postgres
	Users    *userrepo.Repo
	Expenses *expenserepo.Repo
}

func NewEnv(t *testing.T) Env {
	t.Helper()

	rawURL := os.Getenv("TEST_DATABASE_URL")
	if rawURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping database test")
	}

	name := databaseName(t, rawURL)
	if !strings.HasSuffix(name, "_test") {
		t.Fatalf("TEST_DATABASE_URL must point to a database ending with _test, got %q", name)
	}

	ctx := context.Background()
	pg, err := postgres.New(ctx, rawURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(pg.Close)

	if err := pg.CheckSchema(ctx); err != nil {
		t.Fatalf("test database schema: %v", err)
	}
	if err := pg.Reset(ctx); err != nil {
		t.Fatalf("reset test database: %v", err)
	}

	return Env{
		DB:       pg,
		Users:    userrepo.New(pg),
		Expenses: expenserepo.New(pg),
	}
}

func databaseName(t *testing.T, rawURL string) string {
	t.Helper()

	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	return strings.TrimPrefix(parsed.Path, "/")
}
