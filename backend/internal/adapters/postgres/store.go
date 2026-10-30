// Package postgres implements the domain's storage ports on PostgreSQL.
package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/underson/robot-fleet/backend/internal/domain/fleet"
	"github.com/underson/robot-fleet/backend/internal/domain/telemetry"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Store is one adapter for several small ports: the domain asks for exactly
// what each use case needs, and one Postgres-backed type provides all of it.
type Store struct {
	pool *pgxpool.Pool
}

var (
	_ telemetry.Registry     = (*Store)(nil)
	_ telemetry.HistoryStore = (*Store)(nil)
	_ telemetry.StateStore   = (*Store)(nil)
	_ fleet.Reader           = (*Store)(nil)
)

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Migrate applies pending migrations. It runs on startup, so a customer
// upgrading an on-prem install only replaces the binary.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub)
	if err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
