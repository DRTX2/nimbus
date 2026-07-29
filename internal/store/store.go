package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) BootstrapTenant(ctx context.Context, tenantID, name string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenants (id, name) VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name`, tenantID, name)
	if err != nil {
		return fmt.Errorf("bootstrap tenant: %w", err)
	}
	return nil
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}
