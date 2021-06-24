package cache

import (
	"context"
	"github.com/Vertamedia/chproxy/log"
	"github.com/go-redis/redis/v8"
	"time"
)

const PendingTransactionVal = ""

type RedisTransaction struct {
	redisClient redis.UniversalClient
	graceTime   time.Duration
}

func newRedisTransaction(redisClient redis.UniversalClient, graceTime time.Duration) *RedisTransaction {
	return &RedisTransaction{
		redisClient: redisClient,
		graceTime:   graceTime,
	}
}

func (r RedisTransaction) Close() error {
	return r.redisClient.Close()
}

func (r RedisTransaction) Register(key *Key) error {
	return r.redisClient.Set(context.Background(), key.String(), PendingTransactionVal, r.graceTime).Err()
}

func (r RedisTransaction) Unregister(key *Key) error {
	isDone := r.IsDone(key)
	if isDone {
		return nil
	} else {
		return r.redisClient.Del(context.Background(), key.String()).Err()
	}
}

func (r RedisTransaction) IsDone(key *Key) bool {
	value, err := r.redisClient.Get(context.Background(), key.String()).Result()

	if err == redis.Nil {
		return true
	}

	if err != nil {
		log.Errorf("Failed to fetch transaction status from redis for key: %s", key.String())
		return true
	}

	return value != PendingTransactionVal
}
