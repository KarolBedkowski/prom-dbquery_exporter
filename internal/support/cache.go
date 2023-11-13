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
	// Cache with per item expire time
	Cache struct {
		cache     map[string]*cacheItem
		cacheLock sync.Mutex
	}

	cacheItem struct {
		expireTS time.Time
		content  interface{}
	}
)

// NewCache create  new cache object
func NewCache() *Cache {
	return &Cache{
		cache: make(map[string]*cacheItem),
	}
}

// Get key from cache if exists and not expired
func (r *Cache) Get(key string) (interface{}, bool) {
	r.cacheLock.Lock()
	defer r.cacheLock.Unlock()

	ci, ok := r.cache[key]
	if !ok {
		return nil, false
	}

	if ci.expireTS.After(time.Now()) {
		return ci.content, true
	}

	delete(r.cache, key)

	return nil, false
}

// Put data into cache
func (r *Cache) Put(key string, ttl uint, data interface{}) {
	r.cacheLock.Lock()
	r.cache[key] = &cacheItem{
		expireTS: time.Now().Add(time.Duration(ttl) * time.Second),
		content:  data,
	}
	r.cacheLock.Unlock()
}

// Clear whole cache
func (r *Cache) Clear() {
	r.cacheLock.Lock()
	// create new cache using last size as default
	r.cache = make(map[string]*cacheItem, len(r.cache))
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
