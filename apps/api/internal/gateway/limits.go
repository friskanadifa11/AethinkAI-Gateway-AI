package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter enforces the per-key request rate and concurrency ceilings.
//
// The counter lives in Redis so the limit survives a restart and would still be
// correct if the process were ever scaled out. Concurrency is tracked in
// process: an in-flight counter has no meaning across a network hop, and the
// MVP is scoped to a single gateway process.
type Limiter struct {
	sem *semaphoreSet
}

// NewLimiter returns a limiter with no configuration of its own: every value it
// needs comes from the key row, so limits live next to the key they bound.
func NewLimiter() *Limiter {
	return &Limiter{sem: newSemaphoreSet()}
}

// AllowRPM records a request against the key's per-minute budget.
//
// A limit of 0 means unlimited. The window is a fixed minute bucket: cheap,
// explainable, and adequate for a single-operator gateway.
func (l *Limiter) AllowRPM(ctx context.Context, rdb *redis.Client, keyID string, rpm int, now time.Time) (bool, int, error) {
	if rpm <= 0 {
		return true, 0, nil
	}
	bucket := fmt.Sprintf("aethink:rpm:%s:%d", keyID, now.Unix()/60)
	count, err := rdb.Incr(ctx, bucket).Result()
	if err != nil {
		// Failing open would defeat the limit; failing the request closed is
		// the safer default and is visible in the logs.
		return false, 0, err
	}
	if count == 1 {
		rdb.Expire(ctx, bucket, 90*time.Second)
	}
	return count <= int64(rpm), int(count), nil
}

// Acquire reserves one concurrency slot for a key. The returned release must be
// called exactly once.
func (l *Limiter) Acquire(keyID string, maxConcurrency int) (func(), bool) {
	if maxConcurrency <= 0 {
		return func() {}, true
	}
	return l.sem.acquire(keyID, maxConcurrency)
}

// ---- tiny in-process semaphore set -----------------------------------------

type semaphoreSet struct {
	mu    sync.Mutex
	inUse map[string]int
}

func newSemaphoreSet() *semaphoreSet {
	return &semaphoreSet{inUse: map[string]int{}}
}

func (s *semaphoreSet) acquire(key string, max int) (func(), bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inUse[key] >= max {
		return nil, false
	}
	s.inUse[key]++
	released := false
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if released {
			return
		}
		released = true
		if s.inUse[key] > 0 {
			s.inUse[key]--
		}
		if s.inUse[key] == 0 {
			delete(s.inUse, key)
		}
	}, true
}
