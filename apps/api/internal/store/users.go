package store

import (
	"context"
	"errors"
	"time"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
	"github.com/jackc/pgx/v5"
)

// CountAdmins reports how many operators exist. Zero means the installation is
// un-bootstrapped and the console must offer the one-use setup flow.
func (s *Store) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `select count(*) from admin_users`).Scan(&n)
	return n, err
}

// CreateAdmin inserts the first (or an additional) operator.
func (s *Store) CreateAdmin(ctx context.Context, username, passwordHash string) (models.AdminUser, error) {
	var u models.AdminUser
	err := s.Pool.QueryRow(ctx, `
		insert into admin_users (username, password_hash)
		values ($1, $2)
		returning id, username, created_at, last_login_at`,
		username, passwordHash,
	).Scan(&u.ID, &u.Username, &u.CreatedAt, &u.LastLogin)
	if isConflict(err) {
		return u, ErrConflict
	}
	return u, err
}

// Credentials returns the stored password hash for sign-in.
func (s *Store) Credentials(ctx context.Context, username string) (models.AdminUser, string, error) {
	var u models.AdminUser
	var hash string
	err := s.Pool.QueryRow(ctx, `
		select id, username, created_at, last_login_at, password_hash
		from admin_users where username = $1`, username,
	).Scan(&u.ID, &u.Username, &u.CreatedAt, &u.LastLogin, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, "", ErrNotFound
	}
	return u, hash, err
}

// TouchLogin stamps a successful sign-in.
func (s *Store) TouchLogin(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `update admin_users set last_login_at = now() where id = $1`, id)
	return err
}

// SetPassword replaces the password hash for an operator.
func (s *Store) SetPassword(ctx context.Context, id, passwordHash string) error {
	tag, err := s.Pool.Exec(ctx, `update admin_users set password_hash = $2 where id = $1`, id, passwordHash)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// CreateSession stores a new session keyed by the token hash, never the token.
func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, expires time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		insert into admin_sessions (token_hash, user_id, expires_at) values ($1, $2, $3)`,
		tokenHash, userID, expires)
	return err
}

// SessionUser resolves a live session, treating expired rows as absent.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (models.AdminUser, error) {
	var u models.AdminUser
	err := s.Pool.QueryRow(ctx, `
		select u.id, u.username, u.created_at, u.last_login_at
		from admin_sessions s join admin_users u on u.id = s.user_id
		where s.token_hash = $1 and s.expires_at > now()`, tokenHash,
	).Scan(&u.ID, &u.Username, &u.CreatedAt, &u.LastLogin)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// DeleteSession revokes a single session server-side.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.Pool.Exec(ctx, `delete from admin_sessions where token_hash = $1`, tokenHash)
	return err
}

// DeleteOtherSessions backs "sign out everywhere".
func (s *Store) DeleteOtherSessions(ctx context.Context, userID, keepTokenHash string) error {
	_, err := s.Pool.Exec(ctx,
		`delete from admin_sessions where user_id = $1 and token_hash <> $2`, userID, keepTokenHash)
	return err
}

// PurgeExpiredSessions is safe to call on a timer.
func (s *Store) PurgeExpiredSessions(ctx context.Context) error {
	_, err := s.Pool.Exec(ctx, `delete from admin_sessions where expires_at <= now()`)
	return err
}

// Audit appends an operator action to the audit trail.
func (s *Store) Audit(ctx context.Context, actor, action, target string) error {
	_, err := s.Pool.Exec(ctx,
		`insert into audit_events (actor, action, target) values ($1, $2, $3)`, actor, action, target)
	return err
}

// RecentAudit lists the newest audit rows for the Settings page.
func (s *Store) RecentAudit(ctx context.Context, limit int) ([]map[string]any, error) {
	rows, err := s.Pool.Query(ctx, `
		select actor, action, target, created_at from audit_events
		order by created_at desc limit $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var actor, action, target string
		var at time.Time
		if err := rows.Scan(&actor, &action, &target, &at); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"actor": actor, "action": action, "target": target, "created_at": at,
		})
	}
	return out, rows.Err()
}
