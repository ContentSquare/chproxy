package cache

import (
	"fmt"
	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
	"github.com/go-redis/redis"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RedisCache represents a Redis cache.
type RedisCache struct {
	// Name is cache name.
	Name string

	connection *redis.Client

	maxSize   uint64
	expire    time.Duration
	graceTime time.Duration

	pendingEntries     map[string]pendingEntry
	pendingEntriesLock sync.Mutex

	stats Stats

	wg     sync.WaitGroup
	stopCh chan struct{}
}

//NewRedisCache returns new redis cache for the given cfg.
func NewRedisCache(cfg config.Cache) (*RedisCache, error) {
	if len(cfg.RedisHost) == 0 {
		return nil, fmt.Errorf("`RedisHost` cannot be empty")
	}
	var redisPort = 6379

	if cfg.RedisPort != 0 {
		redisPort = cfg.RedisPort
	}

	if cfg.Expire <= 0 {
		return nil, fmt.Errorf("`expire` must be positive")
	}

	graceTime := time.Duration(cfg.GraceTime)
	if graceTime == 0 {
		// Default grace time.
		graceTime = 5 * time.Second
	}
	if graceTime < 0 {
		// Disable protection from `dogpile effect`.
		graceTime = 0
	}
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%v:%v", cfg.RedisHost, redisPort),
		Password: "",          // no password set
		DB:       cfg.RedisDB, // use default DB
	})

	c := &RedisCache{
		Name: cfg.Name,

		connection: client,

		maxSize:   uint64(cfg.MaxSize),
		expire:    time.Duration(cfg.Expire),
		graceTime: graceTime,

		pendingEntries: make(map[string]pendingEntry),
		stopCh:         make(chan struct{}),
	}
	log.Infof("Connected to Redis %v:%v db %v", cfg.RedisHost, redisPort, cfg.RedisDB)

	if err := client.Ping(); err.Val() != "PONG" {
		return nil, fmt.Errorf("unable to connect to redis %v:%v %s", cfg.RedisHost, cfg.RedisPort, err)
	}

	return c, nil
}

// WriteTo writes cached response for the given key to rw.
//
// Returns ErrMissing if the response isn't found in the cache.
func (c *RedisCache) WriteTo(rw http.ResponseWriter, key *Key) error {
	return c.writeTo(rw, key, http.StatusOK)
}

func (c *RedisCache) writeTo(rw http.ResponseWriter, key *Key, statusCode int) error {
	data, ttl, err := c.get(key)
	if err != nil {
		return err
	}

	if err := sendResponseFromRedis(rw, data, ttl, c.expire, statusCode); err != nil {
		return fmt.Errorf("cache %q: %s", c.Name, err)
	}

	return nil
}

func (c *RedisCache) registerPendingEntry(keyName string) bool {
	if c.graceTime <= 0 {
		return true
	}

	c.pendingEntriesLock.Lock()
	_, exists := c.pendingEntries[keyName]
	if !exists {
		c.pendingEntries[keyName] = pendingEntry{
			deadline: time.Now().Add(c.graceTime),
		}
	}
	c.pendingEntriesLock.Unlock()
	return !exists
}

func (c *RedisCache) get(key *Key) ([]byte, time.Duration, error) {
	keyName := key.String()
	keyTTL, _ := c.connection.TTL(keyName).Result()
	keyData, _ := c.connection.Get(keyName).Bytes()
	startTime := time.Now()

again:
	if len(keyData) == 0 {
		// The entry doesn't exist. Signal the caller that it must
		// create the entry.
		if c.registerPendingEntry(keyName) {
			return nil, -1, ErrMissing
		}
		// The entry has been already requested in a concurrent request.
		if time.Since(startTime) > c.graceTime {
			// The entry didn't appear during graceTime.
			// Let the caller creating it.
			return nil, -1, ErrMissing
		}
		// Wait for graceTime in the hope the entry will appear
		// in the cache.
		//
		// This should protect from thundering herd problem when
		// a single slow query is executed from concurrent requests.
		d := 100 * time.Millisecond
		if d > c.graceTime {
			d = c.graceTime
		}
		time.Sleep(d)
		goto again
	}
	return keyData, keyTTL, nil
}

// sendResponseFromFile sends response to rw from f.
//
// Sets 'FileCache-Control: max-age' header if expire > 0.
// Sets the given response status code.
func sendResponseFromRedis(rw http.ResponseWriter, data []byte, ttl time.Duration, expire time.Duration, statusCode int) error {
	h := rw.Header()

	reader := strings.NewReader(string(data))
	ct, err := readHeader(reader)
	if err != nil {
		return fmt.Errorf("cannot read Content-Type from %s", err)
	}
	if len(ct) > 0 {
		h.Set("Content-Type", ct)
	}
	ce, err := readHeader(reader)
	if err != nil {
		return fmt.Errorf("cannot read Content-Encoding from %s", err)
	}
	if len(ce) > 0 {
		h.Set("Content-Encoding", ce)
	}

	// Determine Content-Length
	off, err := reader.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("cannot determine the current position in %s", err)
	}
	cl := int64(len(data)) - off
	h.Set("Content-Length", fmt.Sprintf("%d", cl))

	// Set 'FileCache-Control: max-age' on non-temporary file
	if expire > 0 {
		left := expire - (time.Duration(ttl) * time.Second)
		if ttl > 0 {
			leftSeconds := uint(left / time.Second)
			h.Set("FileCache-Control", fmt.Sprintf("max-age=%d", leftSeconds))
		}
	}

	rw.WriteHeader(statusCode)
	if _, err := io.Copy(rw, reader); err != nil {
		return fmt.Errorf("cannot send to client: %s", err)
	}
	return nil
}

// Close stops the cache.
//
// The cache may be used after it is stopped, but it is no longer cleaned.
func (c *RedisCache) Close() {
	log.Debugf("cache %q: stopping", c.Name)
	close(c.stopCh)
	c.wg.Wait()
	log.Debugf("cache %q: stopped", c.Name)
	c.connection.Close()
	log.Infof("Closed connection to Redis %v", c.connection.Info())
}

// NewResponseWriter wraps rw into cached response writer
// that automatically caches the response under the given key.
//
// The rw must implement http.CloseNotifier.
//
// Commit or Rollback must be called on the returned response writer
// after it is no longer needed.
func (c *RedisCache) NewResponseWriter(rw http.ResponseWriter, key *Key) (ResponseWriter, error) {
	//f, err := ioutil.TempFile(c.dir, "tmp")
	//if err != nil {
	//	return nil, fmt.Errorf("cache %q: cannot create temporary file in %q: %s", c.Name, c.dir, err)
	//}
	return &RedisResponseWriter{
		ResponseWriter: rw,

		key: key,
		c:   c,

		//bw:      bufio.NewWriter(),
	}, nil
}

func (c *RedisCache) unregisterPendingEntry(keyName string) {
	if c.graceTime <= 0 {
		return
	}

	c.pendingEntriesLock.Lock()
	delete(c.pendingEntries, keyName)
	c.pendingEntriesLock.Unlock()
}

func (c *RedisCache) clean() {
	return
}

func (c *RedisCache) MaxSize() uint64 {
	return 0
}

// Stats returns cache stats.
//
// The returned stats is approximate.
func (c *RedisCache) Stats() Stats {
	var s Stats
	s.Size = atomic.LoadUint64(&c.stats.Size)
	s.Items = atomic.LoadUint64(&c.stats.Items)
	return s
}
