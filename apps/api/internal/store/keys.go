package store

import (
	"context"
	"errors"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/jackc/pgx/v5"
)

const keyCols = `id, label, owner_label, prefix, enabled, revoked_at, expires_at,
	rpm, max_concurrency, token_quota, tokens_used, allowed_aliases, created_at, last_used_at`

func scanKey(row pgx.Row) (models.ClientKey, error) {
	var k models.ClientKey
	err := row.Scan(&k.ID, &k.Label, &k.OwnerLabel, &k.Prefix, &k.Enabled, &k.RevokedAt,
		&k.ExpiresAt, &k.RPM, &k.MaxConc, &k.TokenQuota, &k.TokensUsed,
		&k.Allowlist, &k.CreatedAt, &k.LastUsedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

// ListClientKeys returns every key record. The stored hash is never selected.
func (s *Store) ListClientKeys(ctx context.Context) ([]models.ClientKey, error) {
	rows, err := s.Pool.Query(ctx, `select `+keyCols+` from client_keys order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ClientKey{}
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// KeyInput is the create shape.
type KeyInput struct {
	Label      string
	OwnerLabel string
	RPM        int
	MaxConc    int
	TokenQuota int64
	ExpiresAt  *time.Time
	Allowlist  []string
	Hash       string
	Prefix     string
}

// CreateClientKey stores the hash of a freshly minted key.
func (s *Store) CreateClientKey(ctx context.Context, in KeyInput) (models.ClientKey, error) {
	if in.Allowlist == nil {
		in.Allowlist = []string{}
	}
	var id string
	err := s.Pool.QueryRow(ctx, `
		insert into client_keys (label, owner_label, key_hash, prefix, rpm, max_concurrency,
			token_quota, expires_at, allowed_aliases)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9) returning id`,
		in.Label, in.OwnerLabel, in.Hash, in.Prefix, in.RPM, in.MaxConc,
		in.TokenQuota, in.ExpiresAt, in.Allowlist).Scan(&id)
	if isConflict(err) {
		return models.ClientKey{}, ErrConflict
	}
	if err != nil {
		return models.ClientKey{}, err
	}
	return s.GetClientKey(ctx, id)
}

// GetClientKey loads one key record.
func (s *Store) GetClientKey(ctx context.Context, id string) (models.ClientKey, error) {
	return scanKey(s.Pool.QueryRow(ctx, `select `+keyCols+` from client_keys where id = $1`, id))
}

// AuthKey carries the fields the public endpoint needs to authenticate a key.
type AuthKey struct {
	ID         string
	Label      string
	Hash       string
	Enabled    bool
	RevokedAt  *time.Time
	ExpiresAt  *time.Time
	RPM        int
	MaxConc    int
	TokenQuota int64
	TokensUsed int64
	Allowlist  []string
}

// KeyByPrefix resolves a candidate key by its non-secret prefix, then the
// caller compares the hash in constant time. Candidates are tried, because two
// keys may share a prefix.
func (s *Store) KeyByPrefix(ctx context.Context, prefix string) ([]AuthKey, error) {
	rows, err := s.Pool.Query(ctx, `
		select id, label, key_hash, enabled, revoked_at, expires_at, rpm, max_concurrency,
			token_quota, tokens_used, allowed_aliases
		from client_keys where prefix = $1`, prefix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuthKey{}
	for rows.Next() {
		var k AuthKey
		if err := rows.Scan(&k.ID, &k.Label, &k.Hash, &k.Enabled, &k.RevokedAt, &k.ExpiresAt,
			&k.RPM, &k.MaxConc, &k.TokenQuota, &k.TokensUsed, &k.Allowlist); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// RevokeClientKey disables a key immediately. Rows are kept so historical
// request attribution survives.
func (s *Store) RevokeClientKey(ctx context.Context, id string) error {
	tag, err := s.Pool.Exec(ctx,
		`update client_keys set enabled = false, revoked_at = now() where id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetClientKeyEnabled toggles a key without revoking it.
func (s *Store) SetClientKeyEnabled(ctx context.Context, id string, enabled bool) error {
	tag, err := s.Pool.Exec(ctx,
		`update client_keys set enabled = $2, revoked_at = case when $2 then null else now() end where id = $1`,
		id, enabled)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ScheduleKeyExpiry applies a rotation grace window: the previous key stops
// working at the given instant but stays visible for attribution.
func (s *Store) ScheduleKeyExpiry(ctx context.Context, id string, at time.Time) error {
	_, err := s.Pool.Exec(ctx,
		`update client_keys set expires_at = $2 where id = $1 and expires_at is null`, id, at)
	return err
}

// RecordKeyUsage advances the last-used stamp and metered tokens.
func (s *Store) RecordKeyUsage(ctx context.Context, id string, tokens int64) error {
	_, err := s.Pool.Exec(ctx,
		`update client_keys set last_used_at = now(), tokens_used = tokens_used + $2 where id = $1`,
		id, tokens)
	return err
}

// CountActiveKeys feeds the Overview metric strip.
func (s *Store) CountActiveKeys(ctx context.Context) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx,
		`select count(*) from client_keys where enabled and (expires_at is null or expires_at > now())`).Scan(&n)
	return n, err
}

// KeyUpdate is the editable shape of a key. The secret is absent on purpose:
// only its hash exists, so a key's value can never be changed in place - that
// is what rotation is for.
type KeyUpdate struct {
	Label      string
	OwnerLabel string
	RPM        int
	MaxConc    int
	TokenQuota int64
	Allowlist  []string
	Enabled    bool
	// ExpiresAt nil means "never"; the caller decides, because an expired key
	// and a key with no expiry are different states.
	ExpiresAt *time.Time
}

// UpdateClientKey rewrites the editable fields and returns the stored record.
func (s *Store) UpdateClientKey(ctx context.Context, id string, in KeyUpdate) (models.ClientKey, error) {
	if in.Allowlist == nil {
		in.Allowlist = []string{}
	}
	tag, err := s.Pool.Exec(ctx, `
		update client_keys set
			label = $2, owner_label = $3, rpm = $4, max_concurrency = $5,
			token_quota = $6, allowed_aliases = $7, enabled = $8, expires_at = $9,
			revoked_at = case when $8 then null else revoked_at end
		where id = $1`,
		id, in.Label, in.OwnerLabel, in.RPM, in.MaxConc, in.TokenQuota,
		in.Allowlist, in.Enabled, in.ExpiresAt)
	if err != nil {
		return models.ClientKey{}, err
	}
	if tag.RowsAffected() == 0 {
		return models.ClientKey{}, ErrNotFound
	}
	return s.GetClientKey(ctx, id)
}

// AliasUsage is one row of a key's per-model rollup.
type AliasUsage struct {
	Alias    string `json:"alias"`
	Requests int    `json:"requests"`
	Tokens   int64  `json:"tokens"`
}

// KeyUsage is what one customer has done in a window. It is derived from the
// request log rather than from counters on the key, so a key that predates the
// current window still reports honestly.
type KeyUsage struct {
	Since     time.Time    `json:"since"`
	Requests  int          `json:"requests"`
	OK        int          `json:"ok"`
	Errors    int          `json:"errors"`
	TokensIn  int64        `json:"tokens_in"`
	TokensOut int64        `json:"tokens_out"`
	Aliases   []AliasUsage `json:"aliases"`
}

// KeyUsage rolls up one key's traffic since the given instant.
func (s *Store) KeyUsage(ctx context.Context, keyID string, since time.Time) (KeyUsage, error) {
	out := KeyUsage{Since: since, Aliases: []AliasUsage{}}
	err := s.Pool.QueryRow(ctx, `
		select count(*),
		       count(*) filter (where status = 'ok'),
		       count(*) filter (where status <> 'ok'),
		       coalesce(sum(input_tokens), 0),
		       coalesce(sum(output_tokens), 0)
		from requests
		where key_id = $1 and created_at >= $2`, keyID, since).
		Scan(&out.Requests, &out.OK, &out.Errors, &out.TokensIn, &out.TokensOut)
	if err != nil {
		return out, err
	}
	rows, err := s.Pool.Query(ctx, `
		select alias, count(*), coalesce(sum(input_tokens + output_tokens), 0)
		from requests
		where key_id = $1 and created_at >= $2
		group by alias
		order by count(*) desc, alias`, keyID, since)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AliasUsage
		if err := rows.Scan(&a.Alias, &a.Requests, &a.Tokens); err != nil {
			return out, err
		}
		out.Aliases = append(out.Aliases, a)
	}
	return out, rows.Err()
}
