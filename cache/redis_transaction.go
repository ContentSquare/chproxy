package cache

import (
	"context"
	"encoding/json"
	"github.com/go-redis/redis/v8"
	"time"
)

type RedisTransaction struct {
	redisClient redis.UniversalClient
	graceTime time.Duration
}

func newRedisTransaction(redisClient redis.UniversalClient, graceTime time.Duration) *RedisTransaction {
	return &RedisTransaction{
		redisClient: redisClient,
		graceTime: graceTime,
	}
}

func (r RedisTransaction) Close() error {
	return r.redisClient.Close()
}

// todo binary encoding
func (r RedisTransaction) Register(key *Key) error {
	redisCachedValue := &RedisCachedValue{State: PENDING}
	j, _ := json.Marshal(redisCachedValue)
	return r.redisClient.Set(context.Background(), key.String(), string(j), r.graceTime).Err()
}

// count on redis ttl
func (r RedisTransaction) Unregister(key *Key) error {
	return nil
}

func (r RedisTransaction) IsDone(key *Key) bool {
	value, err := r.redisClient.Get(context.Background(), key.String()).Result()

	if err == redis.Nil {
		return true
	}

	// todo check types of errors
	if err != nil {
		return false
	}

	var redisCachedValue RedisCachedValue
	err = json.Unmarshal([]byte(value), &redisCachedValue)

	if err != nil {
		return false
	}

	return redisCachedValue.State == OK
}


const (
	OK = "OK"
	NOK = "NOK"
	PENDING = "PENDING"
)

type RedisCachedValue struct {
	State string `json:"state"`
	Value []byte `json:"value,omitempty"`
}