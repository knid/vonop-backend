//go:build integration

// Package database integration helper: spins up a fresh Postgres container
// per test via testcontainers-go and returns a *Database wired against it.
//
// Gated behind //go:build integration so the default `go test ./...` does
// not require Docker. Pair with NewTestController in internal/controllers
// for end-to-end controller tests.
package database

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

// NewTestDB starts a fresh Postgres 16 container, opens a Bun DB against it,
// installs the uuid-ossp extension (most models default to uuid_generate_v4()),
// and creates a table for each model in modelObjs via Bun's IfNotExists path.
//
// Pass model pointers as `(*models.User)(nil)`, etc. The container is
// terminated automatically via t.Cleanup.
//
// Requires Docker to be running. Skip the test (or the whole package) if
// the Docker daemon is unavailable.
func NewTestDB(t *testing.T, modelObjs ...interface{}) *Database {
	t.Helper()

	ctx := context.Background()

	pg, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("wemob_test"),
		tcpostgres.WithUsername("wemob"),
		tcpostgres.WithPassword("wemob"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("NewTestDB: start postgres container: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := pg.Terminate(ctx); err != nil {
			t.Logf("NewTestDB: terminate container: %v", err)
		}
	})

	dsn, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("NewTestDB: connection string: %v", err)
	}

	// Use bun's pgdriver — same path as production (see internal/bootstrap).
	sqldb := sql.OpenDB(pgdriver.NewConnector(
		pgdriver.WithDSN(dsn),
		pgdriver.WithInsecure(true),
		pgdriver.WithDialTimeout(5*time.Second),
		pgdriver.WithReadTimeout(30*time.Second),
		pgdriver.WithWriteTimeout(10*time.Second),
	))

	if err := sqldb.PingContext(ctx); err != nil {
		t.Fatalf("NewTestDB: ping: %v", err)
	}

	bundb := bun.NewDB(sqldb, pgdialect.New())

	if _, err := bundb.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"`); err != nil {
		t.Fatalf("NewTestDB: uuid-ossp extension: %v", err)
	}

	for _, m := range modelObjs {
		if _, err := bundb.NewCreateTable().Model(m).IfNotExists().Exec(ctx); err != nil {
			t.Fatalf("NewTestDB: create table for %T: %v", m, err)
		}
	}

	return NewDatabase(bundb)
}
