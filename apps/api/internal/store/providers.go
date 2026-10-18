package store

import (
	"context"
	"errors"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/jackc/pgx/v5"
)

const providerCols = `p.id, p.name, p.adapter, p.base_url, p.enabled, p.timeout_ms,
	p.credential_enc is not null as credential_set,
	p.last_test_at, p.last_test_state, p.last_test_error,
	p.created_at, p.updated_at,
	coalesce(array_agg(m.model_id) filter (where m.model_id is not null), '{}') as models`

func scanProvider(row pgx.Row) (models.Provider, error) {
	var p models.Provider
	err := row.Scan(&p.ID, &p.Name, &p.Adapter, &p.BaseURL, &p.Enabled, &p.TimeoutMS,
		&p.CredentialSet, &p.LastTestAt, &p.LastTestState, &p.LastTestError,
		&p.CreatedAt, &p.UpdatedAt, &p.Models)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// ListProviders returns every provider with its declared model list.
func (s *Store) ListProviders(ctx context.Context) ([]models.Provider, error) {
	rows, err := s.Pool.Query(ctx, `
		select `+providerCols+`
		from providers p left join provider_models m on m.provider_id = p.id
		group by p.id order by p.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Provider{}
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProvider loads one provider.
func (s *Store) GetProvider(ctx context.Context, id string) (models.Provider, error) {
	return scanProvider(s.Pool.QueryRow(ctx, `
		select `+providerCols+`
		from providers p left join provider_models m on m.provider_id = p.id
		where p.id = $1
		group by p.id`, id))
}

// ProviderInput is the write shape for a provider.
type ProviderInput struct {
	Name        string
	Adapter     string
	BaseURL     string
	Enabled     bool
	TimeoutMS   int
	Models      []string
	Credential  string // plaintext; empty means "leave existing credential alone"
	SetCredFlag bool
}

// CreateProvider stores a new upstream. The credential is encrypted by the
// caller and only its fingerprint is kept for display.
func (s *Store) CreateProvider(ctx context.Context, in ProviderInput, credentialEnc, fingerprint string) (models.Provider, error) {
	var id string
	err := s.Pool.QueryRow(ctx, `
		insert into providers (name, adapter, base_url, enabled, timeout_ms, credential_enc, credential_fp)
		values ($1, $2, $3, $4, $5, nullif($6,''), nullif($7,''))
		returning id`,
		in.Name, in.Adapter, in.BaseURL, in.Enabled, in.TimeoutMS, credentialEnc, fingerprint).Scan(&id)
	if isConflict(err) {
		return models.Provider{}, ErrConflict
	}
	if err != nil {
		return models.Provider{}, err
	}
	if len(in.Models) > 0 {
		if err := s.SetProviderModels(ctx, id, in.Models); err != nil {
			return models.Provider{}, err
		}
	}
	return s.GetProvider(ctx, id)
}

// UpdateProvider edits an upstream. A blank credential clears nothing: callers
// must pass SetCredFlag to change it, so an accidental empty field cannot wipe
// a working key.
func (s *Store) UpdateProvider(ctx context.Context, id string, in ProviderInput, credentialEnc, fingerprint string) (models.Provider, error) {
	query := `update providers set name=$2, adapter=$3, base_url=$4, enabled=$5, timeout_ms=$6, updated_at=now()`
	args := []any{id, in.Name, in.Adapter, in.BaseURL, in.Enabled, in.TimeoutMS}
	if in.SetCredFlag {
		query += `, credential_enc=nullif($7,''), credential_fp=nullif($8,'')`
		args = append(args, credentialEnc, fingerprint)
	}
	query += ` where id=$1`
	tag, err := s.Pool.Exec(ctx, query, args...)
	if err != nil {
		if isConflict(err) {
			return models.Provider{}, ErrConflict
		}
		return models.Provider{}, err
	}
	if tag.RowsAffected() == 0 {
		return models.Provider{}, ErrNotFound
	}
	if in.Models != nil {
		if err := s.SetProviderModels(ctx, id, in.Models); err != nil {
			return models.Provider{}, err
		}
	}
	return s.GetProvider(ctx, id)
}

// SetProviderModels replaces the provider's model list.
func (s *Store) SetProviderModels(ctx context.Context, id string, models []string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `delete from provider_models where provider_id = $1`, id); err != nil {
		return err
	}
	for _, m := range models {
		if _, err := tx.Exec(ctx,
			`insert into provider_models (provider_id, model_id) values ($1, $2) on conflict do nothing`, id, m); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteProvider removes an upstream. Referenced providers are refused so a
// published route can never be left pointing at nothing.
func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	var refs int
	if err := s.Pool.QueryRow(ctx,
		`select count(*) from route_targets where provider_id = $1`, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return ErrConflict
	}
	tag, err := s.Pool.Exec(ctx, `delete from providers where id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ProviderCredential returns the sealed credential for dispatch.
func (s *Store) ProviderCredential(ctx context.Context, id string) (string, error) {
	var enc *string
	err := s.Pool.QueryRow(ctx, `select credential_enc from providers where id = $1`, id).Scan(&enc)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if enc == nil {
		return "", nil
	}
	return *enc, nil
}

// RecordProviderTest stores the outcome of a bounded connection check.
func (s *Store) RecordProviderTest(ctx context.Context, id, state, errMsg string) error {
	_, err := s.Pool.Exec(ctx, `
		update providers set last_test_at = now(), last_test_state = $2, last_test_error = $3
		where id = $1`, id, state, errMsg)
	return err
}

// ProviderRef is the routing-time view of a provider: only what dispatch needs.
type ProviderRef struct {
	ID            string
	Name          string
	BaseURL       string
	Adapter       string
	TimeoutMS     int
	Enabled       bool
	CredentialEnc string
}

// EnabledProvidersByID loads the dispatch view for the given ids.
func (s *Store) EnabledProvidersByID(ctx context.Context, ids []string) (map[string]ProviderRef, error) {
	out := map[string]ProviderRef{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := s.Pool.Query(ctx, `
		select id, name, base_url, adapter, timeout_ms, enabled, coalesce(credential_enc,'')
		from providers where id = any($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ProviderRef
		if err := rows.Scan(&p.ID, &p.Name, &p.BaseURL, &p.Adapter, &p.TimeoutMS, &p.Enabled, &p.CredentialEnc); err != nil {
			return nil, err
		}
		out[p.ID] = p
	}
	return out, rows.Err()
}

// MarkProviderTouched exists so the updated_at column moves on credential
// rotation even when no other field changed.
func (s *Store) MarkProviderTouched(ctx context.Context, id string, at time.Time) error {
	_, err := s.Pool.Exec(ctx, `update providers set updated_at = $2 where id = $1`, id, at)
	return err
}
