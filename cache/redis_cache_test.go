package cache

import (
	"strings"
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

	buffer := strings.NewReader("an object")

	if _, err := redisCache.Put(buffer, ContentMetadata{}, &Key{Query: []byte("SELECT 1")}); err != nil {
		t.Fatalf("failed to put it to cache: %s", err)
	}
	if _, err := redisCache.Put(buffer, ContentMetadata{}, &Key{Query: []byte("SELECT 2")}); err != nil {
		t.Fatalf("failed to put it to cache: %s", err)
	}
	if _, err := redisCache.Put(buffer, ContentMetadata{}, &Key{Query: []byte("SELECT 2")}); err != nil {
		t.Fatalf("failed to put it to cache: %s", err)
	}

	stats := redisCache.Stats()
	if stats.Items != 2 {
		t.Fatalf("cache should contain 2 items")
	}
	// because of the use of miniredis to simulate a real redis server
	// we can't check stats.Size because miniredis doesn't handle the memory usage of redis
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
	defer func() {
		c.Close()
	}()
	cacheAddGetHelper(t, c)
}

func TestRedisCacheMiss(t *testing.T) {
	c := generateRedisClientAndServer(t)
	cacheMissHelper(t, c)
}
