// Package config reads the process configuration from the environment. Every
// value has a safe default except the credentials the process cannot invent.
package config

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved process configuration.
type Config struct {
	Addr        string
	DatabaseURL string
	RedisURL    string
	// Secret keys the AES-GCM wrapped provider credentials and the HMAC used
	// for client-key hashing. Must be stable across restarts.
	Secret []byte
	// BootstrapToken authorises the one-use first-admin creation. Empty means
	// bootstrap is closed.
	BootstrapToken string
	WebDist        string
	SessionTTL     time.Duration
	DevCookies     bool
	LogLevel       string
	// AllowPrivateUpstreams permits loopback/private provider base URLs. A
	// self-hosted gateway normally talks to an upstream on its own network, so
	// this defaults on; the cloud metadata range is refused regardless.
	AllowPrivateUpstreams bool
}

// Load resolves configuration and reports the first unusable value instead of
// starting a half-configured gateway.
func Load() (*Config, error) {
	cfg := &Config{
		// HTTP_ADDR is the PRD's name; ADDR is accepted as a shorthand.
		Addr:           env("HTTP_ADDR", env("ADDR", "127.0.0.1:8787")),
		DatabaseURL:    env("DATABASE_URL", ""),
		RedisURL:       redisURL(),
		BootstrapToken: env("BOOTSTRAP_TOKEN", ""),
		WebDist:        env("WEB_DIST", ""),
		SessionTTL:     time.Duration(envInt("SESSION_TTL_HOURS", 72)) * time.Hour,
		DevCookies:     envBool("INSECURE_COOKIES", false),
		LogLevel:       env("LOG_LEVEL", "info"),

		AllowPrivateUpstreams: envBool("ALLOW_PRIVATE_UPSTREAMS", true),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.RedisURL == "" {
		return nil, fmt.Errorf("REDIS_URL is required")
	}

	secret := env("APP_SECRET", "")
	if secret == "" {
		// The PRD names the same material by purpose; accept either so the
		// documented .env works unchanged.
		secret = env("CREDENTIAL_ENCRYPTION_KEY", env("SESSION_SECRET", ""))
	}
	if secret == "" {
		// Refusing to boot beats silently generating an ephemeral key that
		// would make every stored credential undecryptable on restart.
		return nil, fmt.Errorf("APP_SECRET (or CREDENTIAL_ENCRYPTION_KEY) is required (generate one with: openssl rand -base64 32)")
	}
	if len(secret) < 24 {
		return nil, fmt.Errorf("APP_SECRET must be at least 24 characters")
	}
	cfg.Secret = []byte(secret)

	// Binding every interface is an explicit opt-in: the default is loopback.
	if exposesAllInterfaces(cfg.Addr) && !envBool("ALLOW_PUBLIC_BIND", false) {
		return nil, fmt.Errorf("ADDR %q would expose the API on every interface; set ALLOW_PUBLIC_BIND=true to confirm", cfg.Addr)
	}
	return cfg, nil
}

// redisURL accepts either a full URL or the host:port form the PRD documents.
func redisURL() string {
	if v := env("REDIS_URL", ""); v != "" {
		return v
	}
	if addr := env("REDIS_ADDR", ""); addr != "" {
		return "redis://" + addr
	}
	return ""
}

func exposesAllInterfaces(addr string) bool {
	return strings.HasPrefix(addr, "0.0.0.0") || strings.HasPrefix(addr, "[::]") ||
		strings.HasPrefix(addr, ":")
}

// GenerateSecret returns a fresh base64 secret, used by the setup script.
func GenerateSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf)
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}
