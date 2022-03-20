package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"

	"github.com/contentsquare/chproxy/config"
)

const cacheTTL = time.Duration(10 * time.Second)

var redisConf = config.Cache{
	Name: "foobar",
	Redis: config.RedisCacheConfig{
		Addresses: []string{"http://localhost:8080"},
	},
	Expire: config.Duration(cacheTTL),
}

func TestCacheSize(t *testing.T) {
	redisCache := generateRedisClientAndServer(t)
	nbKeys := redisCache.nbOfKeys()
	if nbKeys > 0 {
		t.Fatalf("the cache should be empty")
	}

	cacheSize := redisCache.nbOfBytes()
	if cacheSize > 0 {
		t.Fatalf("the cache should be empty")
	}
}

func generateRedisClientAndServer(t *testing.T) *redisCache {
	s := miniredis.RunT(t)
	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{s.Addr()},
	})
	redisCache := newRedisCache(redisClient, redisConf)
	return redisCache
}

func TestRedisCacheAddGet(t *testing.T) {
	c := generateRedisClientAndServer(t)
	c1 := generateRedisClientAndServer(t)
	defer c1.Close()
	cacheAddGetHelper(t, c, c1)
}

func TestRedisCacheMiss(t *testing.T) {
	c := generateRedisClientAndServer(t)
	cacheMissHelper(t, c)
}
