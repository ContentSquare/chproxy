package cache

import (
	"fmt"
	"time"

	"github.com/contentsquare/chproxy/clients"
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/redis/go-redis/v9"
)

// AsyncCache is a transactional cache enabled to serve the results from concurrent queries.
// When query A and B are equal, and query B arrives after query A with no more than defined deadline interval [[graceTime]],
// query B will await for the results of query B for the max time equal to:
// max_awaiting_time = graceTime - (arrivalB - arrivalA)
type AsyncCache struct {
	Cache
	TransactionRegistry

	graceTime time.Duration

	MaxPayloadSize     config.ByteSize
	SharedWithAllUsers bool
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

func (c *AsyncCache) AwaitForConcurrentTransaction(key *Key) (TransactionStatus, error) {
	startTime := time.Now()
	seenState := transactionAbsent
	for {
		elapsedTime := time.Since(startTime)
		if elapsedTime > c.graceTime {
			// The entry didn't appear during deadline.
			// Let the caller creating it.
			return TransactionStatus{State: seenState}, nil
		}

		status, err := c.TransactionRegistry.Status(key)

		if err != nil {
			return TransactionStatus{State: seenState}, err
		}

		if !status.State.IsPending() {
			return status, nil
		}

		// Wait for deadline in the hope the entry will appear
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

func NewAsyncCache(cfg config.Cache, maxExecutionTime time.Duration) (*AsyncCache, error) {
	graceTime := time.Duration(cfg.GraceTime)
	if graceTime > 0 {
		log.Errorf("[DEPRECATED] detected grace time configuration %s. It will be removed in the new version",
			graceTime)
	}
	if graceTime == 0 {
		// Default grace time.
		graceTime = maxExecutionTime
	}
	if graceTime < 0 {
		// Disable protection from `dogpile effect`.
		graceTime = 0
	}

	var cache Cache
	var transaction TransactionRegistry
	var err error
	// transaction will be kept until we're sure there's no possible concurrent query running
	transactionDeadline := 2 * graceTime

	switch cfg.Mode {
	case "file_system":
		cache, err = newFilesSystemCache(cfg, graceTime)
		transaction = newInMemoryTransactionRegistry(transactionDeadline, transactionEndedTTL)
	case "redis":
		var redisClient redis.UniversalClient
		redisClient, err = clients.NewRedisClient(cfg.Redis)
		cache = newRedisCache(redisClient, cfg)
		transaction = newRedisTransactionRegistry(redisClient, transactionDeadline, transactionEndedTTL)
	default:
		return nil, fmt.Errorf("unknown config mode")
	}

	if err != nil {
		return nil, err
	}

	maxPayloadSize := cfg.MaxPayloadSize

	return &AsyncCache{
		Cache:               cache,
		TransactionRegistry: transaction,
		graceTime:           graceTime,
		MaxPayloadSize:      maxPayloadSize,
		SharedWithAllUsers:  cfg.SharedWithAllUsers,
	}, nil
}
