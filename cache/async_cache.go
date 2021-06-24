package cache

import (
	"github.com/Vertamedia/chproxy/config"
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

	fsCache, err := newFSCache(cfg, graceTime)

	if err != nil {
		panic("can not instanciate file system cache")
	}

	return &AsyncCache{
		Cache:       fsCache,
		Transaction: newInMemoryTransaction(graceTime),
		graceTime: graceTime,
	}
}
