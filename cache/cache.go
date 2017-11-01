package cache

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
)

// Cache represents a file cache.
type Cache struct {
	name      string
	dir       string
	maxSize   uint64
	expire    time.Duration
	graceTime time.Duration

	pendingEntries     map[string]pendingEntry
	pendingEntriesLock sync.Mutex

	// Stats from the last clean call
	stats Stats

	wg     sync.WaitGroup
	stopCh chan struct{}
}

type pendingEntry struct {
	deadline time.Time
}

// Stats represents cache stats
type Stats struct {
	// Size is the cache size in bytes.
	Size uint64

	// Items is the number of items in the cache.
	Items uint64
}

// Key is the key for use in the cache.
type Key struct {
	// Query must contain full request query.
	Query []byte

	// IsGzip must be set to true if request header contains
	// 'Accept-Encoding: gzip'.
	IsGzip bool

	// DefaultFormat must contain `default_format` query arg.
	DefaultFormat string

	// Database must contain `database` query arg.
	Database string
}

// String returns string representation of the key.
func (k *Key) String() string {
	s := fmt.Sprintf("Query=%q, IsGzip=%v, DefaultFormat=%q, Database=%q", k.Query, k.IsGzip, k.DefaultFormat, k.Database)
	h := sha256.Sum256([]byte(s))

	// The first 16 bytes of the hash should be enough
	// for collision prevention :)
	return hex.EncodeToString(h[:16])
}

// This regexp must match Key.String output
var cachefileRegexp = regexp.MustCompile(`^[0-9a-f]{32}$`)

// New returns new map of caches created from the given configs.
//
// The map is keyed by cache name.
func New(cfgs []config.Cache) (map[string]*Cache, error) {
	caches := make(map[string]*Cache, len(cfgs))
	for _, cfg := range cfgs {
		if _, ok := caches[cfg.Name]; ok {
			return nil, fmt.Errorf("duplicate cache name %q", cfg.Name)
		}
		c, err := newCache(cfg)
		if err != nil {
			return nil, fmt.Errorf("cannot initialize cache %q: %s", cfg.Name, err)
		}
		caches[cfg.Name] = c
	}
	return caches, nil
}

// Stats returns cache stats.
func (c *Cache) Stats() Stats {
	var s Stats
	s.Size = atomic.LoadUint64(&c.stats.Size)
	s.Items = atomic.LoadUint64(&c.stats.Items)
	return s
}

func newCache(cfg config.Cache) (*Cache, error) {
	if len(cfg.Dir) == 0 {
		return nil, fmt.Errorf("`dir` cannot be empty")
	}
	if cfg.MaxSize <= 0 {
		return nil, fmt.Errorf("`max_size` must be positive")
	}
	if cfg.Expire <= 0 {
		return nil, fmt.Errorf("`expire` must be positive")
	}

	graceTime := cfg.GraceTime
	if graceTime <= 0 {
		graceTime = 0
	}

	c := &Cache{
		name:      cfg.Name,
		dir:       cfg.Dir,
		maxSize:   uint64(cfg.MaxSize),
		expire:    cfg.Expire,
		graceTime: graceTime,

		pendingEntries: make(map[string]pendingEntry),
		stopCh:         make(chan struct{}),
	}

	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create %q: %s", c.dir, err)
	}

	c.wg.Add(1)
	go func() {
		c.cleaner()
		c.wg.Done()
	}()

	c.wg.Add(1)
	go func() {
		c.pendingEntriesCleaner()
		c.wg.Done()
	}()

	return c, nil
}

// Close stops the cache.
//
// The cache mustn't be used after it is stopped.
func (c *Cache) Close() {
	close(c.stopCh)
	c.wg.Wait()
}

func (c *Cache) cleaner() {
	d := c.expire / 2
	if d < time.Minute {
		d = time.Minute
	}
	if d > time.Hour {
		d = time.Hour
	}

	for {
		c.clean()
		select {
		case <-time.After(d):
		case <-c.stopCh:
			return
		}
	}
}

func (c *Cache) clean() {
	currentTime := time.Now()

	// Remove cached files after a graceTime from their expiration,
	// so they may be served until they are substituted with fresh files.
	expire := c.expire + c.graceTime

	// Calculate total cache size and remove expired files.
	var totalSize uint64
	var totalItems uint64
	walkDir(c.dir, func(fi os.FileInfo) {
		mt := fi.ModTime()
		if currentTime.Sub(mt) > expire {
			fn := fi.Name()
			if err := os.Remove(fn); err != nil {
				log.Errorf("cache %q: cannot remove file %q: %s", c.name, fn, err)
			}
			return
		}
		totalSize += uint64(fi.Size())
		totalItems++
	})

	loopsCount := 0
	rnd := rand.New(rand.NewSource(0))
	for totalSize > c.maxSize && loopsCount < 3 {
		// Remove some files in order to reduce cache size.
		excessSize := totalSize - c.maxSize
		p := int32(float64(excessSize) / float64(totalSize) * 100)
		// Remove +10% over totalSize.
		p += 10
		walkDir(c.dir, func(fi os.FileInfo) {
			if rnd.Int31n(100) > p {
				return
			}

			fs := uint64(fi.Size())
			fn := fi.Name()
			if err := os.Remove(fn); err != nil {
				log.Errorf("cache %q: cannot remove file %q: %s", c.name, fn, err)
				return
			}
			totalSize -= fs
			totalItems--
		})

		// This should protect from infinite loop.
		loopsCount++
	}

	atomic.StoreUint64(&c.stats.Size, totalSize)
	atomic.StoreUint64(&c.stats.Items, totalItems)
}

// walkDir calls f on all the cache files in the given dir.
func walkDir(dir string, f func(fi os.FileInfo)) error {
	// Do not use filepath.Walk, since it is inefficient
	// for large number of files.
	// See https://golang.org/pkg/path/filepath/#Walk .
	fd, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot open %q: %s", dir, err)
	}
	defer fd.Close()

	for {
		fis, err := fd.Readdir(1024)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("cannot read files in %q: %s", dir, err)
		}
		for _, fi := range fis {
			if fi.IsDir() {
				// Skip subdirectories
				continue
			}
			fn := fi.Name()
			if !cachefileRegexp.MatchString(fn) {
				// Skip invalid filenames
				continue
			}
			f(fi)
		}
	}
}

// WriteTo writes cached response for the given key to rw.
//
// Returns ErrMissing if the response isn't found in the cache.
func (c *Cache) WriteTo(rw http.ResponseWriter, key *Key) error {
	f, err := c.get(key)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := io.Copy(rw, f); err != nil {
		return fmt.Errorf("cache %q: cannot send %q to client: %s", c.name, f.Name(), err)
	}
	return nil
}

func (c *Cache) get(key *Key) (*os.File, error) {
	fp := c.filepath(key)

	startTime := time.Now()

again:
	f, err := os.Open(fp)
	if err != nil {
		if !os.IsNotExist(err) {
			// Unexpected error.
			return nil, fmt.Errorf("cache %q: cannot open %q: %s", c.name, fp, err)
		}

		// The entry doesn't exist. Signal the caller that it must
		// create the entry.
		if c.registerPendingEntry(fp) {
			return nil, ErrMissing
		}

		// The entry has been already requested in a concurrent request.
		if time.Since(startTime) > c.graceTime {
			// The entry didn't appear during graceTime.
			// Let the caller creating it.
			return nil, ErrMissing
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

	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("cache %q: cannot stat %q: %s", c.name, fp, err)
	}
	mt := fi.ModTime()
	age := time.Since(mt)
	if age > c.expire {
		if age > c.expire+c.graceTime || c.registerPendingEntry(fp) {
			f.Close()
			return nil, ErrMissing
		}
		// Serve expired file in the hope it will be substituted
		// with the fresh file during graceTime.
	}
	return f, nil
}

// ErrMissing is returned when the entry isn't found in the cache.
var ErrMissing = errors.New("missing cache entry")

func (c *Cache) registerPendingEntry(filepath string) bool {
	if c.graceTime <= 0 {
		return true
	}

	c.pendingEntriesLock.Lock()
	_, exists := c.pendingEntries[filepath]
	if !exists {
		c.pendingEntries[filepath] = pendingEntry{
			deadline: time.Now().Add(c.graceTime),
		}
	}
	c.pendingEntriesLock.Unlock()
	return !exists
}

func (c *Cache) unregisterPendingEntry(filepath string) {
	if c.graceTime <= 0 {
		return
	}

	c.pendingEntriesLock.Lock()
	delete(c.pendingEntries, filepath)
	c.pendingEntriesLock.Unlock()
}

func (c *Cache) pendingEntriesCleaner() {
	if c.graceTime <= 0 {
		return
	}

	d := c.graceTime
	if d < 100*time.Millisecond {
		d = 100 * time.Millisecond
	}
	if d > time.Second {
		d = time.Second
	}

	for {
		currentTime := time.Now()

		// Clear outdated pending entries, since they may remain here
		// forever if unregisterPendingEntry call is missing.
		c.pendingEntriesLock.Lock()
		for filepath, pe := range c.pendingEntries {
			if currentTime.After(pe.deadline) {
				delete(c.pendingEntries, filepath)
			}
		}
		c.pendingEntriesLock.Unlock()

		select {
		case <-time.After(d):
		case <-c.stopCh:
			return
		}
	}
}

func (c *Cache) filepath(key *Key) string {
	k := key.String()
	return filepath.Join(c.dir, k)
}

// NewResponseWriter wraps rw into cached response writer
// that automatically caches the response under the given key.
//
// Commit or Rollback must be called on the returned response writer
// after it is no longer needed.
func (c *Cache) NewResponseWriter(rw http.ResponseWriter, key *Key) (*ResponseWriter, error) {
	f, err := ioutil.TempFile(c.dir, "tmp")
	if err != nil {
		return nil, fmt.Errorf("cache %q: cannot create temporary file in %q: %s", c.name, c.dir, err)
	}
	return &ResponseWriter{
		ResponseWriter: rw,

		key: key,
		c:   c,

		tmpFile: f,
		bw:      bufio.NewWriter(f),
	}, nil
}

// ResponseWriter caches the response.
//
// Commit or Rollback must be called after the response writer
// is no longer needed.
type ResponseWriter struct {
	http.ResponseWriter // the original response writer

	key *Key
	c   *Cache

	tmpFile *os.File      // temporary file for response streaming
	bw      *bufio.Writer // buffered writer for the temporary file
}

// Write writes b into rw.
func (rw *ResponseWriter) Write(b []byte) (int, error) {
	return rw.bw.Write(b)
}

// Commit stores the response to the cache and writes it
// to the wrapped response writer.
func (rw *ResponseWriter) Commit() error {
	fp := rw.c.filepath(rw.key)
	defer rw.c.unregisterPendingEntry(fp)

	fn := rw.tmpFile.Name()
	if err := rw.bw.Flush(); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot flush data into %q: %s", rw.c.name, fn, err)
	}
	if err := rw.tmpFile.Close(); err != nil {
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot close %q: %s", rw.c.name, fn, err)
	}

	if err := os.Rename(fn, fp); err != nil {
		return fmt.Errorf("cache %q: cannot rename %q to %q: %s", rw.c.name, fn, fp, err)
	}

	return rw.c.WriteTo(rw.ResponseWriter, rw.key)
}

// Rollback writes the response to the wrapped response writer and discards
// it from the cache.
func (rw *ResponseWriter) Rollback() error {
	fp := rw.c.filepath(rw.key)
	defer rw.c.unregisterPendingEntry(fp)

	fn := rw.tmpFile.Name()
	if err := rw.bw.Flush(); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot flush data into %q: %s", rw.c.name, fn, err)
	}

	if _, err := rw.tmpFile.Seek(0, 0); err != nil {
		panic(fmt.Sprintf("BUG: cache %q: cannot seek to the beginning of %q: %s", rw.c.name, fn, err))
	}

	if _, err := io.Copy(rw.ResponseWriter, rw.tmpFile); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot send %q to client: %s", rw.c.name, fn, err)
	}

	if err := rw.tmpFile.Close(); err != nil {
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot close %q: %s", rw.c.name, fn, err)
	}
	if err := os.Remove(fn); err != nil {
		return fmt.Errorf("cache %q: cannot remove %q: %s", rw.c.name, fn, err)
	}
	return nil
}
