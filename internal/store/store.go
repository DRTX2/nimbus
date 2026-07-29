package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type tenantCapabilityContextKey struct{}

// TenantCapability is an opaque proof returned by API-key authentication.
// Its hash is intentionally unavailable to callers and JSON serializers.
type TenantCapability struct {
	hash  [sha256.Size]byte
	valid bool
}

func WithTenantCapability(ctx context.Context, capability TenantCapability) context.Context {
	return context.WithValue(ctx, tenantCapabilityContextKey{}, capability)
}

type Store struct {
	pool         *pgxpool.Pool
	tenantMu     sync.Mutex
	stepCursor   string
	outboxCursor string
}

func (s *Store) beginTenant(ctx context.Context, tenantID string) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id', $1, true)", tenantID); err != nil {
		_ = tx.Rollback(ctx)
		return nil, fmt.Errorf("set tenant context: %w", err)
	}
	if capability, ok := ctx.Value(tenantCapabilityContextKey{}).(TenantCapability); ok && capability.valid {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.api_key_hash', encode($1::bytea, 'hex'), true)", capability.hash[:]); err != nil {
			_ = tx.Rollback(ctx)
			return nil, fmt.Errorf("set tenant capability: %w", err)
		}
	}
	return tx, nil
}

func (s *Store) workerTenants(ctx context.Context, cursor *string) ([]string, error) {
	s.tenantMu.Lock()
	defer s.tenantMu.Unlock()
	rows, err := s.pool.Query(ctx, "SELECT tenant_id FROM public.nimbus_worker_tenants(NULLIF($1, '')::uuid, 100)", *cursor)
	if err != nil {
		return nil, fmt.Errorf("enumerate worker tenants: %w", err)
	}
	defer rows.Close()
	var tenants []string
	for rows.Next() {
		var tenantID string
		if err := rows.Scan(&tenantID); err != nil {
			return nil, fmt.Errorf("scan worker tenant: %w", err)
		}
		tenants = append(tenants, tenantID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate worker tenants: %w", err)
	}
	if len(tenants) > 0 {
		*cursor = tenants[len(tenants)-1]
	}
	return tenants, nil
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
