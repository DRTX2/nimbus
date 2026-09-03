package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrForbidden = errors.New("forbidden")
var ErrConflict = errors.New("conflict")

func (s *Store) BootstrapOwner(ctx context.Context, tenantID, displayName, token string) error {
	hash := sha256.Sum256([]byte(token))
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("begin owner bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var principalID string
	err = tx.QueryRow(ctx, `
		WITH inserted AS (
			INSERT INTO principals (kind, issuer, subject, display_name)
			VALUES ('api_key', 'nimbus:local', $1, $2)
			ON CONFLICT DO NOTHING RETURNING id
		)
		SELECT id FROM inserted UNION ALL
		SELECT id FROM principals WHERE issuer='nimbus:local' AND subject=$1 LIMIT 1`, tenantID, displayName).Scan(&principalID)
	if err != nil {
		return fmt.Errorf("bootstrap owner principal: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO memberships (tenant_id, principal_id, role)
		VALUES ($1, $2, 'owner')
		ON CONFLICT (tenant_id, principal_id) DO NOTHING`, tenantID, principalID); err != nil {
		return fmt.Errorf("bootstrap owner membership: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO api_keys (tenant_id, principal_id, name, prefix, secret_hash)
		VALUES ($1, $2, 'Local bootstrap key', $3, $4)
		ON CONFLICT (secret_hash) DO NOTHING`, tenantID, principalID, tokenPrefix(token), hash[:]); err != nil {
		return fmt.Errorf("bootstrap owner API key: %w", err)
	}
	var systemID string
	err = tx.QueryRow(ctx, `WITH inserted AS (
		INSERT INTO principals (kind,issuer,subject,display_name) VALUES ('system','nimbus:system',$1,'Nimbus automation')
		ON CONFLICT DO NOTHING RETURNING id)
		SELECT id FROM inserted UNION ALL SELECT id FROM principals WHERE issuer='nimbus:system' AND subject=$1 LIMIT 1`, tenantID).Scan(&systemID)
	if err != nil {
		return fmt.Errorf("bootstrap system principal: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (tenant_id,principal_id,role) VALUES ($1,$2,'operator') ON CONFLICT DO NOTHING`, tenantID, systemID); err != nil {
		return fmt.Errorf("bootstrap system membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit owner bootstrap: %w", err)
	}
	return nil
}

func (s *Store) AuthenticateAPIKey(ctx context.Context, token string) (Principal, TenantCapability, error) {
	hash := sha256.Sum256([]byte(token))
	var result Principal
	err := s.pool.QueryRow(ctx, `SELECT principal_id, tenant_id, display_name, membership_role
		FROM public.nimbus_authenticate_api_key($1)`, hash[:]).
		Scan(&result.ID, &result.TenantID, &result.Name, &result.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, TenantCapability{}, ErrForbidden
	}
	if err != nil {
		return Principal{}, TenantCapability{}, fmt.Errorf("authenticate API key: %w", err)
	}
	return result, TenantCapability{hash: hash, valid: true}, nil
}

func (s *Store) AuthenticateOIDC(ctx context.Context, defaultTenantID, issuer, subject string) (Principal, TenantCapability, error) {
	tx, err := s.beginTenant(ctx, defaultTenantID)
	if err != nil {
		return Principal{}, TenantCapability{}, fmt.Errorf("begin OIDC auth: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result Principal
	err = tx.QueryRow(ctx, `
		SELECT p.id, m.tenant_id, p.display_name, m.role
		FROM principals p
		JOIN memberships m ON m.principal_id = p.id
		WHERE p.kind = 'oidc' AND p.issuer = $1 AND p.subject = $2 AND m.tenant_id = $3`,
		issuer, subject, defaultTenantID).Scan(&result.ID, &result.TenantID, &result.Name, &result.Role)

	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			WITH inserted_principal AS (
				INSERT INTO principals (kind, issuer, subject, display_name)
				VALUES ('oidc', $1, $2, $3)
				ON CONFLICT DO NOTHING RETURNING id
			),
			principal AS (
				SELECT id FROM inserted_principal UNION ALL
				SELECT id FROM principals WHERE issuer = $1 AND subject = $2 LIMIT 1
			),
			inserted_membership AS (
				INSERT INTO memberships (tenant_id, principal_id, role)
				SELECT $4, id, 'viewer' FROM principal
				ON CONFLICT DO NOTHING RETURNING principal_id
			)
			SELECT p.id, $4::uuid, p.display_name, 'viewer'
			FROM principal p`, issuer, subject, subject, defaultTenantID).Scan(&result.ID, &result.TenantID, &result.Name, &result.Role)

		if err != nil {
			return Principal{}, TenantCapability{}, fmt.Errorf("auto-provision OIDC: %w", err)
		}

		if err := insertAudit(ctx, tx, result, "oidc.provision", "principal", result.ID, "succeeded", map[string]any{"role": "viewer"}); err != nil {
			return Principal{}, TenantCapability{}, err
		}
	} else if err != nil {
		return Principal{}, TenantCapability{}, fmt.Errorf("query OIDC principal: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Principal{}, TenantCapability{}, fmt.Errorf("commit OIDC auth: %w", err)
	}

	hash := sha256.Sum256([]byte(issuer + "|" + subject))
	return result, TenantCapability{hash: hash, valid: true}, nil
}

func (s *Store) CreateAPIKey(ctx context.Context, actor Principal, name, role, token string, expiresAt *time.Time) (APIKey, error) {
	if role != "owner" && role != "operator" && role != "approver" && role != "viewer" {
		return APIKey{}, fmt.Errorf("invalid role %q", role)
	}
	hash := sha256.Sum256([]byte(token))
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return APIKey{}, fmt.Errorf("begin API key creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var principalID string
	err = tx.QueryRow(ctx, `SELECT public.nimbus_create_api_principal($1)`, name).Scan(&principalID)
	if err != nil {
		return APIKey{}, fmt.Errorf("create API key principal: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (tenant_id, principal_id, role) VALUES ($1, $2, $3)`, actor.TenantID, principalID, role); err != nil {
		return APIKey{}, fmt.Errorf("create API key membership: %w", err)
	}
	var result APIKey
	err = tx.QueryRow(ctx, `
		INSERT INTO api_keys (tenant_id, principal_id, name, prefix, secret_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, name, prefix, expires_at, revoked_at, created_at`,
		actor.TenantID, principalID, name, tokenPrefix(token), hash[:], expiresAt).
		Scan(&result.ID, &result.Name, &result.Prefix, &result.ExpiresAt, &result.RevokedAt, &result.CreatedAt)
	if err != nil {
		return APIKey{}, fmt.Errorf("create API key: %w", err)
	}
	result.Role = role
	if err := insertAudit(ctx, tx, actor, "api_key.create", "api_key", result.ID, "succeeded", map[string]any{"role": role}); err != nil {
		return APIKey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return APIKey{}, fmt.Errorf("commit API key creation: %w", err)
	}
	return result, nil
}

func (s *Store) ListAPIKeys(ctx context.Context, tenantID string) ([]APIKey, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin API key list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT k.id, k.name, k.prefix, m.role, k.expires_at, k.revoked_at, k.created_at
		FROM api_keys k JOIN memberships m ON m.tenant_id = k.tenant_id AND m.principal_id = k.principal_id
		WHERE k.tenant_id = $1 ORDER BY k.created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list API keys: %w", err)
	}
	defer rows.Close()
	result := make([]APIKey, 0)
	for rows.Next() {
		var item APIKey
		if err := rows.Scan(&item.ID, &item.Name, &item.Prefix, &item.Role, &item.ExpiresAt, &item.RevokedAt, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan API key: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit API key list: %w", err)
	}
	return result, nil
}

func (s *Store) RevokeAPIKey(ctx context.Context, actor Principal, id string) error {
	tx, err := s.beginTenant(ctx, actor.TenantID)
	if err != nil {
		return fmt.Errorf("begin API key revocation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE api_keys SET revoked_at = now() WHERE tenant_id = $1 AND id = $2 AND revoked_at IS NULL`, actor.TenantID, id)
	if err != nil {
		return fmt.Errorf("revoke API key: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor, "api_key.revoke", "api_key", id, "succeeded", map[string]any{}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListAuditEvents(ctx context.Context, tenantID string, limit int) ([]AuditEvent, error) {
	tx, err := s.beginTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("begin audit event list: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT id, actor_id, action, resource_type, resource_id, result, metadata, occurred_at
		FROM audit_events WHERE tenant_id = $1 ORDER BY occurred_at DESC LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	defer rows.Close()
	result := make([]AuditEvent, 0)
	for rows.Next() {
		var item AuditEvent
		if err := rows.Scan(&item.ID, &item.ActorID, &item.Action, &item.ResourceType, &item.ResourceID, &item.Result, &item.Metadata, &item.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit audit event list: %w", err)
	}
	return result, nil
}

func insertAudit(ctx context.Context, tx pgx.Tx, actor Principal, action, resourceType, resourceID, result string, metadata map[string]any) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode audit metadata: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_events (tenant_id, actor_id, action, resource_type, resource_id, result, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, actor.TenantID, actor.ID, action, resourceType, resourceID, result, encoded); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func tokenPrefix(token string) string {
	if len(token) <= 10 {
		return token
	}
	return token[:10]
}
