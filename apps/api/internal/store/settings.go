package store

import (
	"context"

	"github.com/aethinkai/aethinkai-gateway/apps/api/internal/models"
)

// DefaultSettings is what an untouched installation reports. Values are
// operational only: no secret has a settings row.
func DefaultSettings() models.Settings {
	return models.Settings{
		SiteName:     "AethinkAI Gateway AI",
		PublicAPIURL: "",
		Timezone:     "UTC",
		TimeoutMS:    60000,
		RetentionDay: 30,
		Motion:       "system",
	}
}

// LoadSettings reads the stored overrides on top of the defaults.
func (s *Store) LoadSettings(ctx context.Context) (models.Settings, error) {
	set := DefaultSettings()
	rows, err := s.Pool.Query(ctx, `select key, value from settings`)
	if err != nil {
		return set, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return set, err
		}
		switch k {
		case "site_name":
			set.SiteName = v
		case "public_api_url":
			set.PublicAPIURL = v
		case "timezone":
			set.Timezone = v
		case "default_timeout_ms":
			set.TimeoutMS = atoiOr(v, set.TimeoutMS)
		case "retention_days":
			set.RetentionDay = atoiOr(v, set.RetentionDay)
		case "motion":
			set.Motion = v
		}
	}
	return set, rows.Err()
}

// SaveSettings upserts only the keys the operator actually sent.
func (s *Store) SaveSettings(ctx context.Context, values map[string]string) error {
	for k, v := range values {
		if _, err := s.Pool.Exec(ctx, `
			insert into settings (key, value, updated_at) values ($1, $2, now())
			on conflict (key) do update set value = excluded.value, updated_at = now()`, k, v); err != nil {
			return err
		}
	}
	return nil
}

func atoiOr(s string, fallback int) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	if n == 0 {
		return fallback
	}
	return n
}
