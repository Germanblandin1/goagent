package ratelimit

import (
	"sync"

	"golang.org/x/time/rate"
)

// limiterMap is a concurrency-safe map of per-key rate limiters.
// Each key gets its own limiter with the configured rate and burst.
type limiterMap struct {
	mu    sync.Mutex
	items map[string]*rate.Limiter
	rps   rate.Limit
	burst int
}

func newLimiterMap(rps rate.Limit, burst int) *limiterMap {
	return &limiterMap{
		items: make(map[string]*rate.Limiter),
		rps:   rps,
		burst: burst,
	}
}

func (m *limiterMap) get(key string) *rate.Limiter {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.items[key]; ok {
		return l
	}
	l := rate.NewLimiter(m.rps, m.burst)
	m.items[key] = l
	return l
}
