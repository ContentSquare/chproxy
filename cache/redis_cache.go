package cache

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/redis/go-redis/v9"
)

type redisCache struct {
	name   string
	client redis.UniversalClient
	expire time.Duration
}

const getTimeout = 2 * time.Second
const removeTimeout = 1 * time.Second
const renameTimeout = 1 * time.Second
const putTimeout = 2 * time.Second
const statsTimeout = 500 * time.Millisecond

// this variable is key to select whether the result should be streamed
// from redis to the http response or if chproxy should first put the
// result from redis in a temporary files before sending it to the http response
const minTTLForRedisStreamingReader = 15 * time.Second

// tmpDir temporary path to store ongoing queries results
const tmpDir = "/tmp"
const redisTmpFilePrefix = "chproxyRedisTmp"

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
	nbBytesToFetch := int64(100 * 1024)
	stringKey := key.String()
	// fetching 100kBytes from redis to be sure to have the full metadata and,
	//  for most of the queries that fetch a few data, the cached results
	val, err := r.client.GetRange(ctx, stringKey, 0, nbBytesToFetch).Result()

	// errors, such as timeouts
	if err != nil {
		log.Errorf("failed to get key %s with error: %s", stringKey, err)
		return nil, ErrMissing
	}
	// if key not found in cache
	if len(val) == 0 {
		return nil, ErrMissing
	}

	ttl, err := r.client.TTL(ctx, stringKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to ttl of key %s with error: %w", stringKey, err)
	}
	b := []byte(val)
	metadata, offset, err := r.decodeMetadata(b)
	if err != nil {
		if errors.Is(err, &RedisCacheCorruptionError{}) {
			log.Errorf("an error happened while handling redis key =%s, err=%s", stringKey, err)
		}
		return nil, err
	}
	if (int64(offset) + metadata.Length) < nbBytesToFetch {
		// the condition is true ony if the bytes fetched contain the metadata + the cached results
		// so we extract from the remaining bytes the cached results
		payload := b[offset:]
		reader := &ioReaderDecorator{Reader: bytes.NewReader(payload)}
		value := &CachedData{
			ContentMetadata: *metadata,
			Data:            reader,
			Ttl:             ttl,
		}

		return value, nil
	}

	return r.readResultsAboveLimit(offset, stringKey, metadata, ttl)
}

func (r *redisCache) readResultsAboveLimit(offset int, stringKey string, metadata *ContentMetadata, ttl time.Duration) (*CachedData, error) {
	// since the cached results in redis are too big, we can't fetch all of them because of the memory overhead.
	// We will create an io.reader that will fetch redis bulk by bulk to reduce the memory usage.
	redisStreamreader := newRedisStreamReader(uint64(offset), r.client, stringKey, metadata.Length)

	// But before that, since the usage of the reader could take time and the object in redis could disappear btw 2 fetches
	// we need to make sure the TTL will be long enough to avoid nasty side effects
	// if the TTL is too short we will put all the data into a file and use it as a streamer
	// nb: it would be better to retry the flow if such a failure happened but this requires a huge refactoring of proxy.go

	if ttl <= minTTLForRedisStreamingReader {
		fileStream, err := newFileWriterReader(tmpDir)
		if err != nil {
			return nil, err
		}
		_, err = io.Copy(fileStream, redisStreamreader)
		if err != nil {
			return nil, err
		}
		err = fileStream.resetOffset()
		if err != nil {
			return nil, err
		}
		value := &CachedData{
			ContentMetadata: *metadata,
			Data:            fileStream,
			Ttl:             ttl,
		}
		return value, nil
	}

	value := &CachedData{
		ContentMetadata: *metadata,
		Data:            &ioReaderDecorator{Reader: redisStreamreader},
		Ttl:             ttl,
	}

	return value, nil
}

// this struct is here because CachedData requires an io.ReadCloser
// but logic in the Get function generates only an io.Reader
type ioReaderDecorator struct {
	io.Reader
}

func (m ioReaderDecorator) Close() error {
	return nil
}
func (r *redisCache) encodeString(s string) []byte {
	n := uint32(len(s))

	b := make([]byte, 0, n+4)
	b = append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	b = append(b, s...)
	return b
}

func (r *redisCache) decodeString(bytes []byte) (string, int, error) {
	if len(bytes) < 4 {
		return "", 0, &RedisCacheCorruptionError{}
	}
	b := bytes[:4]
	n := uint32(b[3]) | (uint32(b[2]) << 8) | (uint32(b[1]) << 16) | (uint32(b[0]) << 24)
	if len(bytes) < int(4+n) {
		return "", 0, &RedisCacheCorruptionError{}
	}
	s := bytes[4 : 4+n]
	return string(s), int(4 + n), nil
}

func (r *redisCache) encodeMetadata(contentMetadata *ContentMetadata) []byte {
	cLength := contentMetadata.Length
	cType := r.encodeString(contentMetadata.Type)
	cEncoding := r.encodeString(contentMetadata.Encoding)
	b := make([]byte, 0, len(cEncoding)+len(cType)+8)
	b = append(b, byte(cLength>>56), byte(cLength>>48), byte(cLength>>40), byte(cLength>>32), byte(cLength>>24), byte(cLength>>16), byte(cLength>>8), byte(cLength))
	b = append(b, cType...)
	b = append(b, cEncoding...)
	return b
}

func (r *redisCache) decodeMetadata(b []byte) (*ContentMetadata, int, error) {
	if len(b) < 8 {
		return nil, 0, &RedisCacheCorruptionError{}
	}
	cLength := uint64(b[7]) | (uint64(b[6]) << 8) | (uint64(b[5]) << 16) | (uint64(b[4]) << 24) | uint64(b[3])<<32 | (uint64(b[2]) << 40) | (uint64(b[1]) << 48) | (uint64(b[0]) << 56)
	offset := 8
	cType, sizeCType, err := r.decodeString(b[offset:])
	if err != nil {
		return nil, 0, err
	}
	offset += sizeCType
	cEncoding, sizeCEncoding, err := r.decodeString(b[offset:])
	if err != nil {
		return nil, 0, err
	}
	offset += sizeCEncoding
	metadata := &ContentMetadata{
		Length:   int64(cLength),
		Type:     cType,
		Encoding: cEncoding,
	}
	return metadata, offset, nil
}

func (r *redisCache) Put(reader io.Reader, contentMetadata ContentMetadata, key *Key) (time.Duration, error) {
	medatadata := r.encodeMetadata(&contentMetadata)

	stringKey := key.String()
	// in order to make the streaming operation atomic, chproxy streams into a temporary key (only known by the current goroutine)
	// then it switches the full result to the "real" stringKey available for other goroutines
	// nolint:gosec // not security sensitve, only used internally.
	random := strconv.Itoa(rand.Int())
	// Redis RENAME is considered to be a multikey operation. In Cluster mode, both oldkey and renamedkey must be in the same hash slot,
	// Refer Redis Documentation here: https://redis.io/commands/rename/
	// To solve this,we need to force the temporary key to be in the same hash slot. We can do this by adding hashtag to the
	// actual part of the temporary key. When the key contains a "{...}" pattern, only the substring between the braces, "{" and "},"
	// is hashed to obtain the hash slot.
	// Refer the hash tags section of Redis documentation here: https://redis.io/docs/reference/cluster-spec/#hash-tags
	stringKeyTmp := "{" + stringKey + "}" + random + "_tmp"

	ctxSet, cancelFuncSet := context.WithTimeout(context.Background(), putTimeout)
	defer cancelFuncSet()
	err := r.client.Set(ctxSet, stringKeyTmp, medatadata, r.expire).Err()
	if err != nil {
		return 0, err
	}
	// we don't fetch all the reader content bulks by bulks to from redis to avoid memory issue
	// if the content is big (which is the case when chproxy users are fetching a lot of data)
	buffer := make([]byte, 2*1024*1024)
	totalByteWrittenExpected := len(medatadata)
	for {
		n, err := reader.Read(buffer)
		// the reader should return an err = io.EOF once it has nothing to read or at the last read call with content.
		// But this is not the case with this reader so we check the condition n == 0 to exit the read loop.
		// We kept the err == io.EOF in the loop in case the behavior of the reader changes

		if n == 0 {
			break
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		ctxAppend, cancelFuncAppend := context.WithTimeout(context.Background(), putTimeout)
		defer cancelFuncAppend()
		totalByteWritten, err := r.client.Append(ctxAppend, stringKeyTmp, string(buffer[:n])).Result()
		if err != nil {
			// trying to clean redis from this partially inserted item
			r.clean(stringKeyTmp)
			return 0, err
		}
		totalByteWrittenExpected += n
		if int(totalByteWritten) != totalByteWrittenExpected {
			// trying to clean redis from this partially inserted item
			r.clean(stringKeyTmp)
			return 0, fmt.Errorf("could not stream the value into redis, only %d bytes were written instead of %d", totalByteWritten, totalByteWrittenExpected)
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	// at this step we know that the item stored in stringKeyTmp is fully written
	// so we can put it to its final stringKey
	ctxRename, cancelFuncRename := context.WithTimeout(context.Background(), renameTimeout)
	defer cancelFuncRename()
	r.client.Rename(ctxRename, stringKeyTmp, stringKey)
	return r.expire, nil
}

func (r *redisCache) clean(stringKey string) {
	delCtx, cancelFunc := context.WithTimeout(context.Background(), removeTimeout)
	defer cancelFunc()
	delErr := r.client.Del(delCtx, stringKey).Err()
	if delErr != nil {
		log.Debugf("redis item was only partially inserted and chproxy couldn't remove the partial result because of %s", delErr)
	} else {
		log.Debugf("redis item was only partially inserted, chproxy was able to remove it")
	}
}

func (r *redisCache) Name() string {
	return r.name
}

type redisStreamReader struct {
	isRedisEOF          bool
	redisOffset         uint64                // the redisOffset that gives the beginning of the next bulk to fetch
	key                 string                // the key of the value we want to stream from redis
	buffer              []byte                // internal buffer to store the bulks fetched from redis
	bufferOffset        int                   // the offset of the buffer that keep were the read() need to start copying data
	client              redis.UniversalClient // the redis client
	expectedPayloadSize int                   // the size of the object the streamer is supposed to read.
	readPayloadSize     int                   // the size of the object currently written by the reader
}

func newRedisStreamReader(offset uint64, client redis.UniversalClient, key string, payloadSize int64) *redisStreamReader {
	bufferSize := uint64(2 * 1024 * 1024)
	return &redisStreamReader{
		isRedisEOF:          false,
		redisOffset:         offset,
		key:                 key,
		bufferOffset:        int(bufferSize),
		buffer:              make([]byte, bufferSize),
		client:              client,
		expectedPayloadSize: int(payloadSize),
	}
}

func (r *redisStreamReader) Read(destBuf []byte) (n int, err error) {
	// the logic is simple:
	// 1) if the buffer still has data to write, it writes it into destBuf without overflowing destBuf
	// 2) if the buffer only has already written data, the StreamRedis refresh the buffer with new data from redis
	// 3) if the buffer only has already written data & redis has no more data to read then StreamRedis sends an EOF err
	bufSize := len(r.buffer)
	bytesWritten := 0
	// case 3) both the buffer & redis were fully consumed, we can tell the reader to stop reading
	if r.bufferOffset >= bufSize && r.isRedisEOF {
		// Because of the way we fetch from redis, we need to do an extra check because we have no way
		// to know if redis is really EOF or if the value was expired from cache while reading it
		if r.readPayloadSize != r.expectedPayloadSize {
			log.Debugf("error while fetching data from redis payload size doesn't match")
			return 0, &RedisCacheError{key: r.key, readPayloadSize: r.readPayloadSize, expectedPayloadSize: r.expectedPayloadSize}
		}
		return 0, io.EOF
	}

	// case 2) the buffer only has already written data, we need to refresh it with redis datas
	if r.bufferOffset >= bufSize {
		if err := r.readRangeFromRedis(bufSize); err != nil {
			return bytesWritten, err
		}
	}

	// case 1) the buffer contains data to write into destBuf
	if r.bufferOffset < bufSize {
		bytesWritten = copy(destBuf, r.buffer[r.bufferOffset:])
		r.bufferOffset += bytesWritten
		r.readPayloadSize += bytesWritten
	}
	return bytesWritten, nil
}

func (r *redisStreamReader) readRangeFromRedis(bufSize int) error {
	ctx, cancelFunc := context.WithTimeout(context.Background(), getTimeout)
	defer cancelFunc()
	newBuf, err := r.client.GetRange(ctx, r.key, int64(r.redisOffset), int64(r.redisOffset+uint64(bufSize))).Result()
	r.redisOffset += uint64(len(newBuf))
	if errors.Is(err, redis.Nil) || len(newBuf) == 0 {
		r.isRedisEOF = true
	}
	// if redis gave less data than asked it means that it reached the end of the value
	if len(newBuf) < bufSize {
		r.isRedisEOF = true
	}

	// others errors, such as timeouts
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Debugf("failed to get key %s with error: %s", r.key, err)
		err2 := &RedisCacheError{key: r.key, readPayloadSize: r.readPayloadSize,
			expectedPayloadSize: r.expectedPayloadSize, rootcause: err}
		return err2
	}

	r.bufferOffset = 0
	r.buffer = []byte(newBuf)
	return nil
}

type fileWriterReader struct {
	f *os.File
}

func newFileWriterReader(dir string) (*fileWriterReader, error) {
	f, err := os.CreateTemp(dir, redisTmpFilePrefix)
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary file in %q: %w", dir, err)
	}
	return &fileWriterReader{
		f: f,
	}, nil
}

func (r *fileWriterReader) Close() error {
	err := r.f.Close()
	if err != nil {
		return err
	}
	return os.Remove(r.f.Name())
}

func (r *fileWriterReader) Read(destBuf []byte) (n int, err error) {
	return r.f.Read(destBuf)
}

func (r *fileWriterReader) Write(p []byte) (n int, err error) {
	return r.f.Write(p)
}

func (r *fileWriterReader) resetOffset() error {
	if _, err := r.f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("cannot reset offset in: %w", err)
	}
	return nil
}

type RedisCacheError struct {
	key                 string
	readPayloadSize     int
	expectedPayloadSize int
	rootcause           error
}

func (e *RedisCacheError) Error() string {
	errorMsg := fmt.Sprintf("error while reading cached result in redis for key %s, only %d bytes of %d were fetched",
		e.key, e.readPayloadSize, e.expectedPayloadSize)
	if e.rootcause != nil {
		errorMsg = fmt.Sprintf("%s, root cause:%s", errorMsg, e.rootcause)
	}
	return errorMsg
}

type RedisCacheCorruptionError struct {
}

func (e *RedisCacheCorruptionError) Error() string {
	return "chproxy can't decode the cached result from redis, it seems to have been corrupted"
}
