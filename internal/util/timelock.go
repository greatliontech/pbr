package util

import (
	"context"
	"sync"
	"time"
)

type TimeLock[V any] struct {
	interval time.Duration
	expireAt time.Time
	value    V
	refresh  func(ctx context.Context, val V) (V, error)
	mu       sync.RWMutex
}

func NewTimeLock[V any](interval time.Duration, refresh func(ctx context.Context, val V) (V, error)) *TimeLock[V] {
	return &TimeLock[V]{
		interval: interval,
		refresh:  refresh,
		expireAt: time.Now().Add(-interval),
	}
}

func (tl *TimeLock[V]) Get(ctx context.Context) (V, error) {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	if time.Now().Before(tl.expireAt) {
		return tl.value, nil
	}
	return tl.refreshValue(ctx)
}

func (tl *TimeLock[V]) refreshValue(ctx context.Context) (V, error) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	// double check expireAt in case another goroutine has updated it
	if time.Now().Before(tl.expireAt) {
		return tl.value, nil
	}
	value, err := tl.refresh(ctx, tl.value)
	if err != nil {
		return tl.value, err
	}
	tl.value = value
	return tl.value, nil
}
