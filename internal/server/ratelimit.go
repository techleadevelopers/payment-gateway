package server

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"payment-gateway/internal/config"
)

type rateLimitStore interface {
	Allow(key string) bool
	AllowN(key string, max int) (bool, time.Time, int)
}

func newConfiguredRateLimiter(cfg *config.Config, windowMs, max int) rateLimitStore {
	if cfg == nil {
		return newRateLimiter(windowMs, max)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.RateLimitBackend)) {
	case "", "memory":
		return newRateLimiter(windowMs, max)
	case "redis":
		if strings.TrimSpace(cfg.RedisURL) == "" {
			slog.Warn("RATE_LIMIT_BACKEND=redis ignored because REDIS_URL is empty; using memory rate limiter")
			return newRateLimiter(windowMs, max)
		}
		slog.Warn("RATE_LIMIT_BACKEND=redis is configured but Redis backend is not linked in this build; using memory rate limiter")
		return newRateLimiter(windowMs, max)
	default:
		slog.Warn("unknown RATE_LIMIT_BACKEND; using memory rate limiter", "backend", cfg.RateLimitBackend)
		return newRateLimiter(windowMs, max)
	}
}

type rateLimiter struct {
	mu       sync.Mutex
	window   time.Duration
	max      int
	counters map[string]rateBucket
}

type rateBucket struct {
	ResetAt time.Time
	Count   int
}

func newRateLimiter(windowMs, max int) *rateLimiter {
	if windowMs <= 0 {
		windowMs = 60000
	}
	if max <= 0 {
		max = 20
	}
	return &rateLimiter{window: time.Duration(windowMs) * time.Millisecond, max: max, counters: make(map[string]rateBucket)}
}

func (l *rateLimiter) Allow(key string) bool {
	allowed, _, _ := l.AllowN(key, l.max)
	return allowed
}

func (l *rateLimiter) AllowN(key string, max int) (bool, time.Time, int) {
	if max <= 0 {
		max = l.max
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b := l.counters[key]
	if b.ResetAt.IsZero() || now.After(b.ResetAt) {
		b = rateBucket{ResetAt: now.Add(l.window)}
	}
	b.Count++
	l.counters[key] = b
	remaining := max - b.Count
	if remaining < 0 {
		remaining = 0
	}
	return b.Count <= max, b.ResetAt, remaining
}
