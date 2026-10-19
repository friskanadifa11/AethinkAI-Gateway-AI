package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/jackc/pgx/v5"
)

// NewTrace records an accepted request together with every upstream attempt.
// Metadata only: no prompt or completion text is ever stored.
type NewTrace struct {
	TraceID       string
	KeyID         *string
	KeyLabel      string
	Alias         string
	ProviderName  string
	UpstreamModel string
	Status        string
	HTTPStatus    int
	DurationMS    int
	TTFTMS        *int
	InputTokens   int
	OutputTokens  int
	Fallbacks     int
	ErrorCode     string
	Streamed      bool
	Attempts      []models.Attempt
}

// InsertTrace persists a request and its attempts atomically.
func (s *Store) InsertTrace(ctx context.Context, t NewTrace) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var id string
	if err := tx.QueryRow(ctx, `
		insert into requests (trace_id, key_id, key_label, alias, provider_name, upstream_model,
			status, http_status, duration_ms, ttft_ms, input_tokens, output_tokens,
			fallback_count, error_code, streamed)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		returning id`,
		t.TraceID, t.KeyID, t.KeyLabel, t.Alias, t.ProviderName, t.UpstreamModel,
		t.Status, t.HTTPStatus, t.DurationMS, t.TTFTMS, t.InputTokens, t.OutputTokens,
		t.Fallbacks, t.ErrorCode, t.Streamed).Scan(&id); err != nil {
		return err
	}
	for i, a := range t.Attempts {
		if _, err := tx.Exec(ctx, `
			insert into request_attempts (request_id, attempt_no, provider_name, upstream_model,
				status, http_status, duration_ms, error_code)
			values ($1,$2,$3,$4,$5,$6,$7,$8)`,
			id, i, a.ProviderName, a.UpstreamModel, a.Status, a.HTTPStatus, a.DurationMS, a.ErrorCode); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RequestFilter narrows the Request Logs page. All fields are optional.
type RequestFilter struct {
	Since  time.Time
	Status string
	Alias  string
	// KeyID narrows the log to one customer. Empty means every key.
	KeyID  string
	Search string
	Limit  int
	Offset int
}

// ListRequests returns one page plus the total matching count, so the console
// can paginate without loading the table into the browser.
func (s *Store) ListRequests(ctx context.Context, f RequestFilter) ([]models.RequestLog, int, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	where := []string{"created_at >= $1"}
	args := []any{f.Since}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if f.Status != "" {
		add("status = $%d", f.Status)
	}
	if f.Alias != "" {
		add("alias = $%d", f.Alias)
	}
	if f.KeyID != "" {
		add("key_id = $%d", f.KeyID)
	}
	if f.Search != "" {
		args = append(args, f.Search)
		where = append(where, fmt.Sprintf(
			"(trace_id ilike '%%' || $%d || '%%' or key_label ilike '%%' || $%d || '%%')",
			len(args), len(args)))
	}
	cond := strings.Join(where, " and ")

	var total int
	if err := s.Pool.QueryRow(ctx, `select count(*) from requests where `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, f.Limit, f.Offset)
	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		select trace_id, key_label, alias, provider_name, upstream_model, status, http_status,
			duration_ms, ttft_ms, input_tokens, output_tokens, fallback_count, error_code,
			streamed, created_at
		from requests where %s
		order by created_at desc limit $%d offset $%d`, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []models.RequestLog{}
	for rows.Next() {
		var r models.RequestLog
		if err := rows.Scan(&r.TraceID, &r.KeyLabel, &r.Alias, &r.ProviderName, &r.UpstreamMod,
			&r.Status, &r.HTTPStatus, &r.DurationMS, &r.TTFTMS, &r.InputTokens, &r.OutputTokens,
			&r.Fallbacks, &r.ErrorCode, &r.Streamed, &r.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// TraceDetail loads a request and its attempt timeline.
func (s *Store) TraceDetail(ctx context.Context, traceID string) (models.RequestDetail, error) {
	var d models.RequestDetail
	var id string
	err := s.Pool.QueryRow(ctx, `
		select id, trace_id, key_id, key_label, alias, provider_name, upstream_model, status,
			http_status, duration_ms, ttft_ms, input_tokens, output_tokens, fallback_count,
			error_code, streamed, created_at
		from requests where trace_id = $1`, traceID,
	).Scan(&id, &d.TraceID, &d.KeyID, &d.KeyLabel, &d.Alias, &d.ProviderName, &d.UpstreamMod,
		&d.Status, &d.HTTPStatus, &d.DurationMS, &d.TTFTMS, &d.InputTokens, &d.OutputTokens,
		&d.Fallbacks, &d.ErrorCode, &d.Streamed, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	rows, err := s.Pool.Query(ctx, `
		select attempt_no, provider_name, upstream_model, status, http_status, duration_ms, error_code, created_at
		from request_attempts where request_id = $1 order by attempt_no`, id)
	if err != nil {
		return d, err
	}
	defer rows.Close()
	d.Attempts = []models.Attempt{}
	for rows.Next() {
		var a models.Attempt
		if err := rows.Scan(&a.AttemptNo, &a.ProviderName, &a.UpstreamModel, &a.Status,
			&a.HTTPStatus, &a.DurationMS, &a.ErrorCode, &a.CreatedAt); err != nil {
			return d, err
		}
		d.Attempts = append(d.Attempts, a)
	}
	return d, rows.Err()
}

// Overview aggregates the Overview page from stored requests. An installation
// with no traffic returns zeroes; nothing here is synthesised.
func (s *Store) Overview(ctx context.Context, since time.Time, bucket string) (models.Overview, error) {
	if bucket != "minute" && bucket != "hour" && bucket != "day" {
		bucket = "hour"
	}
	o := models.Overview{Series: []models.SeriesPt{}, Providers: []models.Provider{}, RecentFails: []models.RequestLog{}}

	err := s.Pool.QueryRow(ctx, `
		select count(*),
		       count(*) filter (where status = 'ok'),
		       count(*) filter (where status <> 'ok'),
		       coalesce(sum(fallback_count), 0),
		       coalesce(percentile_disc(0.95) within group (order by duration_ms), 0),
		       coalesce(sum(input_tokens), 0),
		       coalesce(sum(output_tokens), 0)
		from requests where created_at >= $1`, since,
	).Scan(&o.Requests, &o.Success, &o.Errors, &o.Fallbacks, &o.P95MS, &o.TokensIn, &o.TokensOut)
	if err != nil {
		return o, err
	}

	rows, err := s.Pool.Query(ctx, fmt.Sprintf(`
		select date_trunc('%s', created_at) as b,
		       count(*) filter (where status = 'ok'),
		       count(*) filter (where status <> 'ok')
		from requests where created_at >= $1 group by b order by b`, bucket), since)
	if err != nil {
		return o, err
	}
	defer rows.Close()
	for rows.Next() {
		var p models.SeriesPt
		if err := rows.Scan(&p.T, &p.OK, &p.Failed); err != nil {
			return o, err
		}
		o.Series = append(o.Series, p)
	}
	rows.Close()

	if o.Providers, err = s.ListProviders(ctx); err != nil {
		return o, err
	}
	if o.Routes, err = s.CountPublishedRoutes(ctx); err != nil {
		return o, err
	}
	if o.Keys, err = s.CountActiveKeys(ctx); err != nil {
		return o, err
	}

	fails, err := s.Pool.Query(ctx, `
		select trace_id, key_label, alias, provider_name, upstream_model, status, http_status,
			duration_ms, ttft_ms, input_tokens, output_tokens, fallback_count, error_code, streamed, created_at
		from requests where status <> 'ok' order by created_at desc limit 5`)
	if err != nil {
		return o, err
	}
	defer fails.Close()
	for fails.Next() {
		var r models.RequestLog
		if err := fails.Scan(&r.TraceID, &r.KeyLabel, &r.Alias, &r.ProviderName, &r.UpstreamMod,
			&r.Status, &r.HTTPStatus, &r.DurationMS, &r.TTFTMS, &r.InputTokens, &r.OutputTokens,
			&r.Fallbacks, &r.ErrorCode, &r.Streamed, &r.CreatedAt); err != nil {
			return o, err
		}
		o.RecentFails = append(o.RecentFails, r)
	}
	return o, fails.Err()
}

// DistinctAliases powers the Request Logs filter dropdown.
func (s *Store) DistinctAliases(ctx context.Context) ([]string, error) {
	rows, err := s.Pool.Query(ctx,
		`select distinct alias from requests order by alias limit 200`)
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

// PurgeRequests enforces the retention window.
func (s *Store) PurgeRequests(ctx context.Context, olderThan time.Time) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `delete from requests where created_at < $1`, olderThan)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
