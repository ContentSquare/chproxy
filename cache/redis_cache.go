package cache

import (
	"context"
	"fmt"
	"github.com/Vertamedia/chproxy/config"
	"github.com/go-redis/redis/v8"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

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

	f, err := ioutil.TempFile("/tmp", "tmp")
	if err != nil {
		return fmt.Errorf("cannot create temporary file in /tmp: %s", err)
	}
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()
	if _, err := f.Write([]byte(val)); err != nil {
		return err
	}

	return SendResponseFromFile(w, f, r.expire, 200)
}

// todo set state
func (r *RedisCache) Put(file *os.File, key *Key) (time.Duration, error) {
	data, err := ioutil.ReadFile(file.Name())
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
