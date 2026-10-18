package store

import (
	"context"
	"errors"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/jackc/pgx/v5"
)

// ListRoutes returns every stored version, newest version per alias first, with
// its ordered targets. The Routing page needs both the live version and any
// draft, so filtering happens in the caller.
func (s *Store) ListRoutes(ctx context.Context) ([]models.Route, error) {
	rows, err := s.Pool.Query(ctx, `
		select id, alias, version, published, strategy, created_at, published_at
		from routes order by alias, version desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.Route{}
	byID := map[string]int{}
	for rows.Next() {
		var r models.Route
		if err := rows.Scan(&r.ID, &r.Alias, &r.Version, &r.Published, &r.Strategy, &r.CreatedAt, &r.PublishedAt); err != nil {
			return nil, err
		}
		r.Targets = []models.RouteTarget{}
		byID[r.ID] = len(out)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	trows, err := s.Pool.Query(ctx, `
		select t.route_id, t.position, t.provider_id, p.name, t.enabled, t.upstream_model, t.timeout_ms, t.retries
		from route_targets t join providers p on p.id = t.provider_id
		order by t.route_id, t.position`)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		var routeID string
		var t models.RouteTarget
		if err := trows.Scan(&routeID, &t.Position, &t.ProviderID, &t.ProviderName,
			&t.Enabled, &t.UpstreamModel, &t.TimeoutMS, &t.Retries); err != nil {
			return nil, err
		}
		if idx, ok := byID[routeID]; ok {
			out[idx].Targets = append(out[idx].Targets, t)
		}
	}
	return out, trows.Err()
}

// GetRoute loads one version with its targets.
func (s *Store) GetRoute(ctx context.Context, id string) (models.Route, error) {
	var r models.Route
	err := s.Pool.QueryRow(ctx, `
		select id, alias, version, published, strategy, created_at, published_at
		from routes where id = $1`, id,
	).Scan(&r.ID, &r.Alias, &r.Version, &r.Published, &r.Strategy, &r.CreatedAt, &r.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	rows, err := s.Pool.Query(ctx, `
		select t.position, t.provider_id, p.name, t.enabled, t.upstream_model, t.timeout_ms, t.retries
		from route_targets t join providers p on p.id = t.provider_id
		where t.route_id = $1 order by t.position`, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	r.Targets = []models.RouteTarget{}
	for rows.Next() {
		var t models.RouteTarget
		if err := rows.Scan(&t.Position, &t.ProviderID, &t.ProviderName, &t.Enabled,
			&t.UpstreamModel, &t.TimeoutMS, &t.Retries); err != nil {
			return r, err
		}
		r.Targets = append(r.Targets, t)
	}
	return r, rows.Err()
}

// ResolveRoute returns the single published version for an alias. This is the
// hot path for /v1/chat/completions.
func (s *Store) ResolveRoute(ctx context.Context, alias string) (models.Route, error) {
	var id string
	err := s.Pool.QueryRow(ctx,
		`select id from routes where alias = $1 and published limit 1`, alias).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Route{}, ErrNotFound
	}
	if err != nil {
		return models.Route{}, err
	}
	return s.GetRoute(ctx, id)
}

// CreateRouteVersion stores a new draft. Version numbers are allocated inside
// the transaction so concurrent drafts cannot collide.
func (s *Store) CreateRouteVersion(ctx context.Context, alias, strategy string, targets []models.RouteTarget) (models.Route, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return models.Route{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var version int
	if err := tx.QueryRow(ctx,
		`select coalesce(max(version), 0) + 1 from routes where alias = $1`, alias).Scan(&version); err != nil {
		return models.Route{}, err
	}
	var id string
	if err := tx.QueryRow(ctx, `
		insert into routes (alias, version, strategy, published)
		values ($1, $2, $3, false) returning id`, alias, version, strategy).Scan(&id); err != nil {
		if isConflict(err) {
			return models.Route{}, ErrConflict
		}
		return models.Route{}, err
	}
	for i, t := range targets {
		if _, err := tx.Exec(ctx, `
			insert into route_targets (route_id, position, provider_id, upstream_model, timeout_ms, retries, enabled)
			values ($1, $2, $3, $4, $5, $6, $7)`,
			id, i, t.ProviderID, t.UpstreamModel, t.TimeoutMS, t.Retries, t.Enabled); err != nil {
			if isConflict(err) {
				return models.Route{}, ErrConflict
			}
			return models.Route{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return models.Route{}, err
	}
	return s.GetRoute(ctx, id)
}

// PublishRoute atomically promotes a version: the alias's current live version
// is retired and the target becomes the only published row for that alias. The
// partial unique index makes this safe under concurrency.
func (s *Store) PublishRoute(ctx context.Context, id string) (models.Route, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return models.Route{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var alias string
	if err := tx.QueryRow(ctx, `select alias from routes where id = $1 for update`, id).Scan(&alias); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.Route{}, ErrNotFound
		}
		return models.Route{}, err
	}
	if _, err := tx.Exec(ctx,
		`update routes set published = false where alias = $1 and published`, alias); err != nil {
		return models.Route{}, err
	}
	if _, err := tx.Exec(ctx,
		`update routes set published = true, published_at = now() where id = $1`, id); err != nil {
		return models.Route{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.Route{}, err
	}
	return s.GetRoute(ctx, id)
}

// DeleteRouteVersion refuses to delete the live version: an alias must always
// keep a working target, so the operator publishes a replacement first.
func (s *Store) DeleteRouteVersion(ctx context.Context, id string) error {
	var published bool
	err := s.Pool.QueryRow(ctx, `select published from routes where id = $1`, id).Scan(&published)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if published {
		return ErrConflict
	}
	_, err = s.Pool.Exec(ctx, `delete from routes where id = $1`, id)
	return err
}

// PublishedAliases lists aliases with a live version, used by /v1/models.
func (s *Store) PublishedAliases(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx, `select alias from routes where published order by alias`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// CountPublishedRoutes feeds the Overview metric strip.
func (s *Store) CountPublishedRoutes(ctx context.Context) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `select count(*) from routes where published`).Scan(&n)
	return n, err
}
