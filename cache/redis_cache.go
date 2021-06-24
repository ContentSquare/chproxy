package cache

import (
	"context"
	"encoding/json"
	"github.com/Vertamedia/chproxy/config"
	"github.com/go-redis/redis/v8"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

type RedisCache struct {
	name 	string
	client  redis.UniversalClient
	expire  time.Duration
}

func newRedisCache(client redis.UniversalClient, cfg config.Cache) *RedisCache {
	redisCache := &RedisCache{
		name: cfg.Name,
		expire: time.Duration(cfg.Expire),
	}

	redisCache.client = client

	return redisCache
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

	if err == redis.Nil {
		return ErrMissing
	}

	var redisCachedValue RedisCachedValue
	err = json.Unmarshal([]byte(val), &redisCachedValue)

	tmpResponseWriter, err := NewTmpFileResponseWriter(w, "/tmp")

	tmpResponseWriter.Write(redisCachedValue.Value)

	if err == nil {
		SendResponseFromFile(w, tmpResponseWriter.tmpFile, r.expire, 200)
	}

	return err
}

// todo set state
func (r *RedisCache) Put(file *os.File, key *Key) (time.Duration, error) {
	data, err := ioutil.ReadFile(file.Name())

	redisCachedValue := RedisCachedValue{State: OK, Value: data}

	j, _ := json.Marshal(redisCachedValue)
	err = r.client.Set(context.Background(), key.String(), j, r.expire).Err()

	if err != nil {
		return 0, err
	}

	return r.expire, nil
}

func (r *RedisCache) Name() string {
	return r.name
}
