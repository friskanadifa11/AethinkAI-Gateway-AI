package gateway

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// Breaker is a per-provider circuit breaker, shared through Redis so every
// replica reaches the same verdict.
//
// Without it a provider that is down stays first in every route, and every
// request pays its full timeout before failing over. The breaker turns that into
// an immediate skip for a cooldown window, then lets a single probe back in: one
// success closes it, one failure re-opens it.
type Breaker struct {
	rdb       *redis.Client
	threshold int
	cooldown  time.Duration
}

const (
	// Three consecutive failures is enough to call a provider down: one is a
	// blip, two is suspicious, three is a pattern.
	defaultBreakerThreshold = 3
	// Thirty seconds keeps a recovered provider usable quickly while still
	// sparing it, and the caller's latency, during an outage.
	defaultBreakerCooldown = 30 * time.Second
	// A failure counter nobody reads again still has to disappear.
	breakerFailureTTL = 10 * time.Minute
)

// NewBreaker returns a breaker with the defaults above. A nil client makes every
// method a no-op, so a gateway without Redis still serves traffic.
func NewBreaker(rdb *redis.Client) *Breaker {
	return &Breaker{rdb: rdb, threshold: defaultBreakerThreshold, cooldown: defaultBreakerCooldown}
}

func breakerKey(providerID, suffix string) string {
	return "aethink:health:" + providerID + ":" + suffix
}

// Available reports whether a provider should be attempted at all.
//
// A read failure answers true: an unreadable breaker must not block traffic that
// would otherwise work.
func (b *Breaker) Available(ctx context.Context, providerID string) bool {
	if b == nil || b.rdb == nil || providerID == "" {
		return true
	}
	n, err := b.rdb.Exists(ctx, breakerKey(providerID, "open")).Result()
	if err != nil {
		return true
	}
	return n == 0
}

// Fail records one failed attempt and reports whether the breaker just opened.
//
// The failure counter survives opening, so the first attempt after the cooldown
// re-opens the breaker immediately instead of needing the full threshold again.
// That is what keeps the post-cooldown probe cheap.
func (b *Breaker) Fail(ctx context.Context, providerID string) bool {
	if b == nil || b.rdb == nil || providerID == "" {
		return false
	}
	failsKey := breakerKey(providerID, "fails")
	n, err := b.rdb.Incr(ctx, failsKey).Result()
	if err != nil {
		return false
	}
	b.rdb.Expire(ctx, failsKey, breakerFailureTTL)
	if int(n) < b.threshold {
		return false
	}
	// The open marker carries the cooldown as its TTL, so closing the breaker
	// needs no sweeper.
	b.rdb.Set(ctx, breakerKey(providerID, "open"), time.Now().UTC().Format(time.RFC3339), b.cooldown)
	return true
}

// Succeed closes the breaker and clears the streak.
func (b *Breaker) Succeed(ctx context.Context, providerID string) {
	if b == nil || b.rdb == nil || providerID == "" {
		return
	}
	b.rdb.Del(ctx, breakerKey(providerID, "fails"), breakerKey(providerID, "open"))
}

// State reports the breaker's view for the console: whether the provider is
// cooling down, how long is left, and the current failure streak.
func (b *Breaker) State(ctx context.Context, providerID string) (open bool, remaining time.Duration, failures int) {
	if b == nil || b.rdb == nil || providerID == "" {
		return false, 0, 0
	}
	if ttl, err := b.rdb.TTL(ctx, breakerKey(providerID, "open")).Result(); err == nil && ttl > 0 {
		open = true
		remaining = ttl
	}
	if n, err := b.rdb.Get(ctx, breakerKey(providerID, "fails")).Int(); err == nil {
		failures = n
	}
	return open, remaining, failures
}
