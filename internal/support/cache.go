package support

//
// cache.go
// Copyright (C) 2021 Karol Będkowski <Karol Będkowski@kkomp>
//
// Distributed under terms of the GPLv3 license.
//

import (
	"sync"
	"time"

	"prom-dbquery_exporter.app/internal/metrics"
)

type (
	// Cache with per item expire time.
	Cache[T any] struct {
		name      string
		cache     map[string]cacheItem[T]
		cacheLock sync.Mutex
	}

	cacheItem[T any] struct {
		expireTS time.Time
		content  T
	}
)

// NewCache create  new cache object.
func NewCache[T any](name string) *Cache[T] {
	return &Cache[T]{
		name:  name,
		cache: make(map[string]cacheItem[T]),
	}
}

// Get key from cache if exists and not expired.
func (r *Cache[T]) Get(key string) (T, bool) {
	r.cacheLock.Lock()
	defer r.cacheLock.Unlock()

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
	r.cacheLock.Lock()
	defer r.cacheLock.Unlock()

	r.cache[key] = cacheItem[T]{
		expireTS: time.Now().Add(ttl),
		content:  data,
	}
}

// Clear whole cache.
func (r *Cache[T]) Clear() {
	r.cacheLock.Lock()
	defer r.cacheLock.Unlock()

	clear(r.cache)
}

// func (r *Cache) purgeExpired() {
// 	r.cacheLock.Lock()
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
// 	r.cacheLock.Unlock()
// }
