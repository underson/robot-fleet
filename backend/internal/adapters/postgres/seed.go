package postgres

import (
	"context"
	_ "embed"
	"fmt"
)

//go:embed seed_demo.sql
var demoSeed string

// SeedDemo loads demo sites and robots. Only for local runs and demos.
func (s *Store) SeedDemo(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, demoSeed); err != nil {
		return fmt.Errorf("seed demo data: %w", err)
	}
	return nil
}
