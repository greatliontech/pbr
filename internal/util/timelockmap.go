package util

import (
	"context"
	"time"

	"github.com/greatliontech/pbr/internal/store/mem"
)

type KVWrapper[K comparable, V any] struct {
	Key   K
	Value V
}

type TimeLockMap[K comparable, V any] struct {
	values   mem.SyncMap[K, *TimeLock[KVWrapper[K, V]]]
	interval time.Duration
	expireAt time.Time
	refresh  func(ctx context.Context, val KVWrapper[K, V]) (KVWrapper[K, V], error)
}

func NewTimeLockMap[K comparable, V any](interval time.Duration, refresh func(ctx context.Context, val KVWrapper[K, V]) (KVWrapper[K, V], error)) *TimeLockMap[K, V] {
	return &TimeLockMap[K, V]{
		interval: interval,
		expireAt: time.Now().Add(-interval),
		refresh:  refresh,
	}
}

func (tlm *TimeLockMap[K, V]) Get(ctx context.Context, key K) (V, error) {
	if cache, ok := tlm.values.Load(key); ok {
		val, err := cache.Get(ctx)
		if err == nil {
			return val.Value, nil
		}
		return val.Value, err
	}
	tl := NewTimeLock[KVWrapper[K, V]](tlm.interval, tlm.refresh)
	tlm.values.Store(key, tl)
	val, err := tl.Get(ctx)
	if err != nil {
		return val.Value, err
	}
	return val.Value, nil
}
