package cache

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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"prom-dbquery_exporter.app/internal/metrics"
)

var (
	queryCacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "cache",
			Name:        "hit_total",
			Help:        "Number of data loaded from cache",
			ConstLabels: nil,
		},
		[]string{"name"},
	)

	queryCacheMiss = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace:   metrics.MetricsNamespace,
			Subsystem:   "cache",
			Name:        "miss_total",
			Help:        "Number of data not found in cache",
			ConstLabels: nil,
		},
		[]string{"name"},
	)
)

//---------------------------------------------------

func init() {
	prometheus.MustRegister(queryCacheHits)
	prometheus.MustRegister(queryCacheMiss)
}

// ---------------------------------------------------

type (
	// Cache with per item expire time.
	Cache[T any] struct {
		log zerolog.Logger

		cache map[string]cacheItem[T]
		name  string

		mu sync.RWMutex
	}

	cacheItem[T any] struct {
		expireTS time.Time
		content  T
	}
)

// New create  new cache object.
func New[T any](name string) Cache[T] {
	return Cache[T]{
		name:  name,
		cache: make(map[string]cacheItem[T]),
		log:   log.Logger.With().Str("subsystem", "cache").Str("cache_name", name).Logger(),
		mu:    sync.RWMutex{},
	}
}

// Get key from cache if exists and not expired.
func (r *Cache[T]) Get(key string) (T, bool) {
	r.log.Debug().Msgf("cache: get %q", key)

	r.mu.RLock()
	defer r.mu.RUnlock()

	item, ok := r.cache[key]
	if !ok {
		queryCacheMiss.WithLabelValues(r.name).Inc()

		return *new(T), false
	}

	if item.expireTS.After(time.Now()) {
		queryCacheHits.WithLabelValues(r.name).Inc()

		return item.content, true
	}

	queryCacheMiss.WithLabelValues(r.name).Inc()

	return *new(T), false
}

// Put data into cache.
func (r *Cache[T]) Put(key string, ttl time.Duration, data T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log.Debug().Msgf("cache: put key=%q; ttl=%s", key, ttl)

	r.cache[key] = cacheItem[T]{
		expireTS: time.Now().Add(ttl),
		content:  data,
	}
}

// Clear whole cache.
func (r *Cache[T]) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.log.Debug().Msg("cache: clear")
	clear(r.cache)
}

func (r *Cache[T]) Content() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	res := make([]string, 0, len(r.cache))
	for k, v := range r.cache {
		res = append(res, fmt.Sprintf("%q: expires: %s", k, v.expireTS))
	}

	return res
}
