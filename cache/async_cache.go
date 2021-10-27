package cache

import (
	"github.com/Vertamedia/chproxy/config"
	"time"
)

// AsyncCache is a transactional cache allowing the results from concurrent queries.
// When query A is equal to query B and A arrives no more than defined graceTime, query A will await for the results of query B for the max time equal to:
// graceTime - (arrivalB - arrivalA)
type AsyncCache struct {
	Cache
	TransactionRegistry

	graceTime time.Duration
}

func (c *AsyncCache) Close() error {
	if c.TransactionRegistry != nil {
		c.TransactionRegistry.Close()
	}
	if c.Cache != nil {
		c.Cache.Close()
	}
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

		ok := c.TransactionRegistry.IsDone(key)
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

func NewAsyncCache(cfg config.Cache) (*AsyncCache, error) {
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
	var transaction TransactionRegistry
	var err error

	switch cfg.Mode {
	case "file_system":
		cache, err = newFilesSystemCache(cfg, graceTime)
		transaction = newInMemoryTransactionRegistry(graceTime)
	}

	if err != nil {
		return nil, err
	}

	return &AsyncCache{
		Cache:               cache,
		TransactionRegistry: transaction,
		graceTime:           graceTime,
	}, nil

}
