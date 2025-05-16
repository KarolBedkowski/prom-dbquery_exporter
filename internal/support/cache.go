package support

//
// cache.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/metrics"
)

type (
	// Cache with per item expire time.
	Cache[T any] struct {
		cache  map[string]cacheItem[T]
		name   string
		lock   sync.Mutex
		logger zerolog.Logger
	}

	cacheItem[T any] struct {
		expireTS time.Time
		content  T
	}
)

// NewCache create  new cache object.
func NewCache[T any](name string) *Cache[T] {
	return &Cache[T]{
		name:   name,
		cache:  make(map[string]cacheItem[T]),
		logger: log.Logger.With().Str("subsystem", "cache").Str("cache_name", name).Logger(),
	}
}

// Get key from cache if exists and not expired.
func (r *Cache[T]) Get(key string) (T, bool) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.logger.Debug().Msgf("get from cache: key=%s", key)

	item, ok := r.cache[key]
	if !ok {
		metrics.IncQueryCacheMiss(r.name)

		return *new(T), false
	}

	if item.expireTS.After(time.Now()) {
		metrics.IncQueryCacheHits(r.name)

		return item.content, true
	}

	delete(r.cache, key)
	metrics.IncQueryCacheMiss(r.name)

	return *new(T), false
}

// Put data into cache.
func (r *Cache[T]) Put(key string, ttl time.Duration, data T) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.logger.Debug().Msgf("put into cache: key=%s; ttl=%s", key, ttl)
	r.cache[key] = cacheItem[T]{
		expireTS: time.Now().Add(ttl),
		content:  data,
	}
}

// Clear whole cache.
func (r *Cache[T]) Clear() {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.logger.Debug().Msg("clear cache")
	clear(r.cache)
}

func (r *Cache[T]) Content() []string {
	r.lock.Lock()
	defer r.lock.Unlock()

	res := make([]string, 0, len(r.cache))
	for k, v := range r.cache {
		res = append(res, fmt.Sprintf("%s: expires: %s", k, v.expireTS))
	}

	return res
}

// func (r *Cache) purgeExpired() {
// 	r.lock.Lock()
// 	var toDel []string
// 	now := time.Now()
// 	for k, v := range r.cache {
// 		if v.expireTS.Before(now) {
// 			toDel = append(toDel, k)
// 		}
// 	}
// 	for _, k := range toDel {
// 		delete(r.cache, k)
// 	}
// 	r.lock.Unlock()
// }
