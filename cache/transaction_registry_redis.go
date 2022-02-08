package cache

import (
	"context"
	"github.com/Vertamedia/chproxy/log"
	"github.com/go-redis/redis/v8"
	"time"
)

// pendingTransactionVal
// Value set in redis that indicates that a query is being computed
const pendingTransactionVal = ""

type redisTransactionRegistry struct {
	redisClient redis.UniversalClient
	graceTime   time.Duration
}

func newRedisTransactionRegistry(redisClient redis.UniversalClient, graceTime time.Duration) *redisTransactionRegistry {
	return &redisTransactionRegistry{
		redisClient: redisClient,
		graceTime:   graceTime,
	}
}

func (r *redisTransactionRegistry) Close() error {
	return r.redisClient.Close()
}

func (r *redisTransactionRegistry) Register(key *Key) error {
	return r.redisClient.Set(context.Background(), key.String(), pendingTransactionVal, r.graceTime).Err()
}

func (r *redisTransactionRegistry) Unregister(key *Key) error {
	isDone := r.IsDone(key)
	if isDone {
		return nil
	} else {
		return r.redisClient.Del(context.Background(), key.String()).Err()
	}
}

func (r *redisTransactionRegistry) IsDone(key *Key) bool {
	value, err := r.redisClient.Get(context.Background(), key.String()).Result()

	if err == redis.Nil {
		return true
	}

	if err != nil {
		log.Errorf("Failed to fetch transaction status from redis for key: %s", key.String())
		return true
	}

	return value != pendingTransactionVal
}
