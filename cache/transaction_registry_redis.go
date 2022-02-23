package cache

import (
	"context"
	"time"

	"github.com/contentsquare/chproxy/log"
	"github.com/go-redis/redis/v8"
)

// pendingTransactionVal
// Value set in redis that indicates that a query is being computed
const pendingTransactionVal = ""

// This interface is a subset of all avialalble function from redis clients
// It contains only the functions we're using and help to mock a client
type RedisClientLight interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Close() error
}

type redisTransactionRegistry struct {
	redisClient RedisClientLight
	graceTime   time.Duration
}

func newRedisTransactionRegistry(redisClient RedisClientLight, graceTime time.Duration) *redisTransactionRegistry {
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
