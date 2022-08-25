package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/go-redis/redis/v8"
)

type redisCache struct {
	name   string
	client redis.UniversalClient
	expire time.Duration
}

const getTimeout = 1 * time.Second
const putTimeout = 2 * time.Second
const statsTimeout = 500 * time.Millisecond

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
	if errors.Is(err, redis.Nil) {
		return nil, ErrMissing
	}

	// others errors, such as timeouts
	if err != nil {
		log.Errorf("failed to get key %s with error: %s", key.String(), err)
		return nil, ErrMissing
	}
	ttl, err := r.client.TTL(ctx, key.String()).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to ttl of key %s with error: %s", key.String(), err)
	}
	content, reader := r.fromByte([]byte(val))

	reader2 := &io_reader_decorator{Reader: reader}
	value := &CachedData{
		ContentMetadata: *content,
		Data:            reader2,
		Ttl:             ttl,
	}

	return value, nil
}

// this struct is here because CachedData requires an io.ReadCloser
// but logic in the the Get function generates only an io.Reader
type io_reader_decorator struct {
	io.Reader
}

func (m io_reader_decorator) Close() error {
	return nil
}
func (r *redisCache) stringToBytes(s string) []byte {
	n := uint32(len(s))

	b := make([]byte, 0, n+4)
	b = append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	b = append(b, s...)
	return b
}

func (r *redisCache) stringFromBytes(bytes []byte) (string, int) {
	b := bytes[:4]
	n := uint32(b[3]) | (uint32(b[2]) << 8) | (uint32(b[1]) << 16) | (uint32(b[0]) << 24)
	s := bytes[4 : 4+n]
	return string(s), int(4 + n)
}

func (r *redisCache) toByte(contentMetadata *ContentMetadata, reader io.Reader) ([]byte, error) {
	data, err := toBytes(reader)
	if err != nil {
		return nil, err
	}
	cLength := contentMetadata.Length
	cType := r.stringToBytes(contentMetadata.Type)
	cEncoding := r.stringToBytes(contentMetadata.Encoding)
	b := make([]byte, 0, len(data)+len(cEncoding)+len(cType)+8)
	b = append(b, byte(cLength>>56), byte(cLength>>48), byte(cLength>>40), byte(cLength>>32), byte(cLength>>24), byte(cLength>>16), byte(cLength>>8), byte(cLength))
	b = append(b, cType...)
	b = append(b, cEncoding...)
	b = append(b, data...)
	return b, nil
}

func (r *redisCache) fromByte(b []byte) (*ContentMetadata, io.Reader) {
	cLength := uint64(b[7]) | (uint64(b[6]) << 8) | (uint64(b[5]) << 16) | (uint64(b[4]) << 24) | uint64(b[3])<<32 | (uint64(b[2]) << 40) | (uint64(b[1]) << 48) | (uint64(b[0]) << 56)
	offset := 8
	cType, sizeCType := r.stringFromBytes(b[offset:])
	offset += sizeCType
	cEncoding, sizeCEncoding := r.stringFromBytes(b[offset:])
	offset += sizeCEncoding
	payload := b[offset:]
	metadata := &ContentMetadata{
		Length:   int64(cLength),
		Type:     cType,
		Encoding: cEncoding,
	}
	return metadata, bytes.NewReader(payload)
}

func (r *redisCache) Put(reader io.Reader, contentMetadata ContentMetadata, key *Key) (time.Duration, error) {

	payload, err := r.toByte(&contentMetadata, reader)
	if err != nil {
		return 0, err
	}
	ctx, cancelFunc := context.WithTimeout(context.Background(), putTimeout)
	defer cancelFunc()
	err = r.client.Set(ctx, key.String(), payload, r.expire).Err()
	if err != nil {
		return 0, err
	}

	return r.expire, nil
}

func (r *redisCache) Name() string {
	return r.name
}

func toBytes(stream io.Reader) ([]byte, error) {
	b, err := io.ReadAll(stream)
	if err != nil {
		return nil, err
	}
	return b, nil

	/*buf := new(bytes.Buffer)

	_, err := buf.ReadFrom(stream)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil*/
}
