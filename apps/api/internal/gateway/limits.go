package gateway

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter enforces the per-key request rate and concurrency ceilings.
//
// Both counters live in Redis. Rate limiting already needed a shared clock; the
// concurrency ceiling needs one too, because an in-process counter silently
// multiplies by the number of replicas — a key capped at four concurrent
// requests would be allowed twelve behind three pods.
type Limiter struct{}

// NewLimiter returns a limiter with no configuration of its own: every value it
// needs comes from the key row, so limits live next to the key they bound.
func NewLimiter() *Limiter {
	return &Limiter{}
}

// AllowRPM records a request against a sliding one-minute window.
//
// A fixed minute bucket lets a caller spend the whole budget in the last second
// of one minute and the whole budget again in the first second of the next: twice
// the configured rate across the boundary. Counting a sorted set of timestamps
// over the true last sixty seconds has no such seam.
//
// ticket identifies this request inside the window; it is removed again when the
// request is refused, so refused calls do not consume budget they never used.
func (l *Limiter) AllowRPM(ctx context.Context, rdb *redis.Client, keyID, ticket string, rpm int, now time.Time) (bool, int, error) {
	if rpm <= 0 {
		return true, 0, nil
	}
	key := "aethink:rpm:" + keyID
	cutoff := strconv.FormatInt(now.Add(-time.Minute).UnixMilli(), 10)

	pipe := rdb.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", cutoff)
	pipe.ZAdd(ctx, key, redis.Z{Score: float64(now.UnixMilli()), Member: ticket})
	pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, 2*time.Minute)
	cmds, err := pipe.Exec(ctx)
	if err != nil {
		// Failing open would defeat the limit; refusing is the safer default
		// and is visible in the logs.
		return false, 0, err
	}
	count := int(cmds[2].(*redis.IntCmd).Val())
	if count > rpm {
		rdb.ZRem(ctx, key, ticket)
		return false, count - 1, nil
	}
	return true, count, nil
}

// Acquire reserves one concurrency slot for a key. The returned release must be
// called exactly once.
//
// The slot is a Redis counter with a TTL. The TTL is not the limit: it is the
// repair. If a replica dies while holding slots, they disappear instead of
// pinning the key at its ceiling forever.
func (l *Limiter) Acquire(ctx context.Context, rdb *redis.Client, keyID string, maxConcurrency int) (func(), bool) {
	if maxConcurrency <= 0 {
		return func() {}, true
	}
	key := "aethink:conc:" + keyID
	n, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		// Same posture as the rate window: without the counter the ceiling
		// cannot be enforced, so the request is refused rather than admitted.
		return nil, false
	}
	if int(n) > maxConcurrency {
		rdb.Decr(ctx, key)
		return nil, false
	}
	rdb.Expire(ctx, key, 5*time.Minute)

	var once sync.Once
	return func() {
		once.Do(func() {
			// Detached: the request may already be cancelled, and a slot that
			// is never given back is worse than a slow release.
			relCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			rdb.Decr(relCtx, key)
		})
	}, true
}
