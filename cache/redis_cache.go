package cache

import (
	"bytes"
	"context"
	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"github.com/go-redis/redis/v8"
	"io"
	"regexp"
	"strconv"
	"time"
)

type redisCache struct {
	name   string
	client redis.UniversalClient
	expire time.Duration
}

func newRedisCache(client redis.UniversalClient, cfg config.Cache) *redisCache {
	redisCache := &redisCache{
		name:   cfg.Name,
		expire: time.Duration(cfg.Expire),
		client: client,
	}

	return redisCache
}

func (r *redisCache) Close() error {
	return r.client.Close()
}

var usedMemoryRegexp = regexp.MustCompile(`used_memory:([0-9]+)\r\n`)

// Stats will make two calls to redis.
// First one fetches the number of keys stored in redis (DBSize)
// Second one will fetch memory info that will be parsed to fetch the used_memory
// NOTE : we can only fetch database size, not cache size
func (r *redisCache) Stats() Stats {
	return Stats{
		Items: r.nbOfKeys(),
		Size:  r.nbOfBytes(),
	}
}

func (r *redisCache) nbOfKeys() uint64 {
	nbOfKeys, err := r.client.DBSize(context.Background()).Result()
	if err != nil {
		log.Errorf("failed to fetch nb of keys in redis: %s", err)
	}
	return uint64(nbOfKeys)
}

func (r *redisCache) nbOfBytes() uint64 {
	memoryInfo, err := r.client.Info(context.Background(), "memory").Result()
	if err != nil {
		log.Errorf("failed to fetch nb of bytes in redis: %s", err)
	}
	matches := usedMemoryRegexp.FindStringSubmatch(memoryInfo)

	var cacheSize int

	if len(matches) > 1 {
		cacheSize, err = strconv.Atoi(matches[1])
		if err != nil {
			log.Errorf("failed to parse memory usage with error %s", err)
		}
	}

	return uint64(cacheSize)
}

func (r *redisCache) Get(key *Key) (*CachedData, error) {
	val, err := r.client.Get(context.Background(), key.String()).Result()

	if err == redis.Nil || val == pendingTransactionVal {
		return nil, ErrMissing
	}

	ttl, err := r.client.TTL(context.Background(), key.String()).Result()
	if err == redis.Nil {
		log.Errorf("Not able to fetch TTL for: %s ", key)
	}

	value := &CachedData{
		Data: bytes.NewReader([]byte(val)),
		Ttl:  ttl,
	}

	return value, nil
}

func (r *redisCache) Put(reader io.ReadSeeker, key *Key) (time.Duration, error) {
	data, err := toBytes(reader)
	if err != nil {
		return 0, err
	}

	err = r.client.Set(context.Background(), key.String(), data, r.expire).Err()

	if err != nil {
		return 0, err
	}

	return r.expire, nil
}

func (r *redisCache) Name() string {
	return r.name
}

func toBytes(stream io.ReadSeeker) ([]byte, error) {
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
