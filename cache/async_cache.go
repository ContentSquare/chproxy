package cache

import (
	"context"
	"github.com/Vertamedia/chproxy/config"
	"github.com/go-redis/redis/v8"
	"time"
)

type AsyncCache struct {
	Cache
	Transaction

	graceTime time.Duration
}

func (c *AsyncCache) Close() error {
	c.Transaction.Close()
	c.Cache.Close()
	return nil
}

func (c *AsyncCache) AwaitForConcurrentTransaction(key *Key) bool {
	startTime := time.Now()

	for {
		if time.Since(startTime) > c.graceTime {
			// The entry didn't appear during graceTime.
			// Let the caller creating it.
			return false
		}

		ok := c.Transaction.IsDone(key)
		if ok {
			return ok
		}

		// Wait for graceTime in the hope the entry will appear
		// in the cache.
		//
		// This should protect from thundering herd problem when
		// a single slow query is executed from concurrent requests.
		d := 100 * time.Millisecond
		if d > c.graceTime {
			d = c.graceTime
		}
		time.Sleep(d)
	}
}

func NewAsyncCache(cfg config.Cache) *AsyncCache {
	graceTime := time.Duration(cfg.GraceTime)
	if graceTime == 0 {
		// Default grace time.
		graceTime = 5 * time.Second
	}
	if graceTime < 0 {
		// Disable protection from `dogpile effect`.
		graceTime = 0
	}

	var cache Cache
	var transaction Transaction
	var err error

	switch cfg.Mode {
	case "fs":
		cache, err = newFSCache(cfg, graceTime)
		transaction = newInMemoryTransaction(graceTime)
	case "redis":
		var redisClient redis.UniversalClient
		redisClient, err = newRedisClient(cfg.Redis)
		cache = newRedisCache(redisClient, cfg)
		transaction = newRedisTransaction(redisClient, time.Duration(cfg.GraceTime))
	}

	if err != nil {
		panic("can not instantiate file system cache")
	}

	return &AsyncCache{
		Cache:       cache,
		Transaction: transaction,
		graceTime: graceTime,
	}

}


func newRedisClient(cfg config.RedisCacheConfig) (redis.UniversalClient, error) {
	r := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: cfg.Addresses,
	})

	err := r.Ping(context.Background()).Err()

	return r, err
}