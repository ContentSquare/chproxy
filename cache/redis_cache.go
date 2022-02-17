package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
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

const getTimeout = 1 * time.Second
const putTimeout = 2 * time.Second
const statsTimeout = 500 * time.Millisecond

type redisCachePayload struct {
	Length   int64  `json:"l"`
	Type     string `json:"t"`
	Encoding string `json:"enc"`
	Payload  string `json:"payload"`
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
	ctx, cancelFunc := context.WithTimeout(context.Background(), statsTimeout)
	defer cancelFunc()
	nbOfKeys, err := r.client.DBSize(ctx).Result()
	if err != nil {
		log.Errorf("failed to fetch nb of keys in redis: %s", err)
	}
	return uint64(nbOfKeys)
}

func (r *redisCache) nbOfBytes() uint64 {
	ctx, cancelFunc := context.WithTimeout(context.Background(), statsTimeout)
	defer cancelFunc()
	memoryInfo, err := r.client.Info(ctx, "memory").Result()
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
	ctx, cancelFunc := context.WithTimeout(context.Background(), getTimeout)
	defer cancelFunc()
	val, err := r.client.Get(ctx, key.String()).Result()

	// if key not found in cache
	if err == redis.Nil || val == pendingTransactionVal {
		return nil, ErrMissing
	}

	// others errors, such as timeouts
	if err != nil {
		log.Errorf("failed to get key %s with error: %s", key.String(), err)
		return nil, ErrMissing
	}

	var payload redisCachePayload
	err = json.Unmarshal([]byte(val), &payload)

	if err != nil {
		log.Errorf("corrupted payload for key %s with error: %s", key.String(), err)
		return nil, ErrMissing
	}

	ttl, err := r.client.TTL(ctx, key.String()).Result()

	if err != nil {
		log.Errorf("Not able to fetch TTL for: %s ", key)
	}

	value := &CachedData{
		ContentMetadata: ContentMetadata{
			Length:   payload.Length,
			Type:     payload.Type,
			Encoding: payload.Encoding,
		},
		Data: bytes.NewReader([]byte(payload.Payload)),
		Ttl:  ttl,
	}

	return value, nil
}

func (r *redisCache) Put(reader io.Reader, contentMetadata ContentMetadata, key *Key) (time.Duration, error) {
	data, err := toBytes(reader)
	if err != nil {
		return 0, err
	}

	payload := &redisCachePayload{
		Length: contentMetadata.Length, Type: contentMetadata.Type, Encoding: contentMetadata.Encoding, Payload: string(data),
	}

	marshalled, err := json.Marshal(payload)
	if err != nil {
		return 0, nil
	}

	ctx, cancelFunc := context.WithTimeout(context.Background(), putTimeout)
	defer cancelFunc()
	err = r.client.Set(ctx, key.String(), marshalled, r.expire).Err()

	if err != nil {
		return 0, err
	}

	return r.expire, nil
}

func (r *redisCache) Name() string {
	return r.name
}

func toBytes(stream io.Reader) ([]byte, error) {
	buf := new(bytes.Buffer)

	_, err := buf.ReadFrom(stream)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
