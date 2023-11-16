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
)

type (
	// Cache with per item expire time.
	Cache[T any] struct {
		cache     map[string]*cacheItem[T]
		cacheLock sync.Mutex
	}

	cacheItem[T any] struct {
		expireTS time.Time
		content  T
	}
)

// NewCache create  new cache object.
func NewCache[T any]() *Cache[T] {
	return &Cache[T]{
		cache: make(map[string]*cacheItem[T]),
	}
}

// Get key from cache if exists and not expired.
func (r *Cache[T]) Get(key string) (T, bool) {
	r.cacheLock.Lock()
	defer r.cacheLock.Unlock()

	item, ok := r.cache[key]
	if !ok {
		return *new(T), false
	}

	if item.expireTS.After(time.Now()) {
		return item.content, true
	}

	delete(r.cache, key)

	return *new(T), false
}

// Put data into cache.
func (r *Cache[T]) Put(key string, ttl uint, data T) {
	r.cacheLock.Lock()
	r.cache[key] = &cacheItem[T]{
		expireTS: time.Now().Add(time.Duration(ttl) * time.Second),
		content:  data,
	}
	r.cacheLock.Unlock()
}

// Clear whole cache.
func (r *Cache[T]) Clear() {
	r.cacheLock.Lock()
	// create new cache using last size as default
	clear(r.cache)
	r.cacheLock.Unlock()
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
