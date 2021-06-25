package cache

import (
	"bytes"
	"context"
	"fmt"
	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/go-redis/redis/v8"
	"io"
	"net/http"
	"time"
)

// todo:
// execute command to set max size of database at startup
// https://redis.io/topics/lru-cache in order to respect max size parameter
type RedisCache struct {
	name   string
	client redis.UniversalClient
	expire time.Duration
}

func newRedisCache(client redis.UniversalClient, cfg config.Cache) *RedisCache {
	redisCache := &RedisCache{
		name:   cfg.Name,
		expire: time.Duration(cfg.Expire),
		client: client,
	}

	return redisCache
}

func newRedisClient(cfg config.RedisCacheConfig) (redis.UniversalClient, error) {
	r := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: cfg.Addresses,
	})

	err := r.Ping(context.Background()).Err()

	if err != nil {
		return nil, fmt.Errorf("failed to reach redis: %w", err)
	}

	return r, nil

}

func (r *RedisCache) Close() error {
	return r.client.Close()
}

// todo implement stats size
func (r *RedisCache) Stats() Stats {
	nbKeys, _ := r.client.DBSize(context.Background()).Result()

	return Stats{
		Items: uint64(nbKeys),
	}
}

func (r *RedisCache) Get(w http.ResponseWriter, key *Key) error {
	val, err := r.client.Get(context.Background(), key.String()).Result()

	if err == redis.Nil || val == PendingTransactionVal {
		return ErrMissing
	}

	ttl, err := r.client.TTL(context.Background(), key.String()).Result()
	if err == redis.Nil {
		log.Errorf("Not able to fetch TTL for: %s ", key)
	}

	return SendResponseFromReader(w, bytes.NewReader([]byte(val)), ttl, 200)
}

func (r *RedisCache) Put(reader io.ReadSeeker, key *Key) (time.Duration, error) {
	data, err := streamToBytes(reader)
	if err != nil {
		return 0, err
	}

	err = r.client.Set(context.Background(), key.String(), data, r.expire).Err()

	if err != nil {
		return 0, err
	}

	return r.expire, nil
}

func (r *RedisCache) Name() string {
	return r.name
}

func streamToBytes(stream io.ReadSeeker) ([]byte, error) {
	buf := new(bytes.Buffer)
	_, err := stream.Seek(0, io.SeekStart)
	if err != nil {
		return nil, err
	}

	_, err = buf.ReadFrom(stream)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
