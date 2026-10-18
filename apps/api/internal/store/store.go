// Package store owns every database and cache access. HTTP handlers never
// build SQL; they call the methods here.
package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound is returned instead of a driver error so handlers can map it to
// 404 without importing pgx.
var ErrNotFound = errors.New("not found")

// ErrConflict reports a uniqueness violation the caller should surface as 409.
var ErrConflict = errors.New("conflict")

// Store bundles the durable and cache connections.
type Store struct {
	Pool *pgxpool.Pool
	RDB  *redis.Client
}

// Open connects to Postgres and Redis and verifies both are usable.
func Open(ctx context.Context, databaseURL, redisURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 8
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		pool.Close()
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Store{Pool: pool, RDB: rdb}, nil
}

// Close releases both connections.
func (s *Store) Close() {
	if s.RDB != nil {
		_ = s.RDB.Close()
	}
	if s.Pool != nil {
		s.Pool.Close()
	}
}

// Healthy reports whether Postgres and Redis both answer. Readiness depends on
// this: the gateway cannot enforce limits or record traces without them.
func (s *Store) Healthy(ctx context.Context) (postgres bool, redisOK bool) {
	postgres = s.Pool != nil && s.Pool.Ping(ctx) == nil
	redisOK = s.RDB != nil && s.RDB.Ping(ctx).Err() == nil
	return
}

// Migrate applies every embedded migration in filename order, recording each
// in schema_migrations so restarts are cheap and idempotent.
func (s *Store) Migrate(ctx context.Context, log *slog.Logger) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	if _, err := s.Pool.Exec(ctx, `create table if not exists schema_migrations (
		version text primary key, applied_at timestamptz not null default now())`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, name := range names {
		var applied bool
		if err := s.Pool.QueryRow(ctx,
			`select exists(select 1 from schema_migrations where version = $1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `insert into schema_migrations(version) values ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		log.Info("migration applied", "version", name)
	}
	return nil
}

// isConflict reports a Postgres unique-violation.
func isConflict(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
