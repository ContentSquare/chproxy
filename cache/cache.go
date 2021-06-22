package cache

import (
	"errors"
	"fmt"
	"io"
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

// cacheVersion must be increased with each backwads-incompatible change
// in the cache storage.
//const cacheVersion = 2

// FSCache represents a file cache.
type FSCache struct {
	// Name is cache name.
	Name string

	dir       string
	maxSize   uint64
	expire    time.Duration
	graceTime time.Duration

	pendingEntries     map[string]pendingEntry
	pendingEntriesLock sync.Mutex

	stats Stats

	wg     sync.WaitGroup
	stopCh chan struct{}
}

// This regexp must match Key.String output
var cachefileRegexp = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Stats returns cache stats.
//
// The returned stats is approximate.
func (c *FSCache) Stats() Stats {
	var s Stats
	s.Size = atomic.LoadUint64(&c.stats.Size)
	s.Items = atomic.LoadUint64(&c.stats.Items)
	return s
}

// New returns new cache for the given cfg.
func New(cfg config.Cache) (*FSCache, error) {
	if len(cfg.Dir) == 0 {
		return nil, fmt.Errorf("`dir` cannot be empty")
	}
	if cfg.MaxSize <= 0 {
		return nil, fmt.Errorf("`max_size` must be positive")
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

	c := &FSCache{
		Name: cfg.Name,

		dir:       cfg.Dir,
		maxSize:   uint64(cfg.MaxSize),
		expire:    time.Duration(cfg.Expire),
		graceTime: graceTime,

		pendingEntries: make(map[string]pendingEntry),
		stopCh:         make(chan struct{}),
	}

	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create %q: %s", c.dir, err)
	}

	c.wg.Add(1)
	go func() {
		log.Debugf("cache %q: cleaner start", c.Name)
		c.cleaner()
		log.Debugf("cache %q: cleaner stop", c.Name)
		c.wg.Done()
	}()

	c.wg.Add(1)
	go func() {
		log.Debugf("cache %q: pendingEntriesCleaner start", c.Name)
		c.pendingEntriesCleaner()
		log.Debugf("cache %q: pendingEntriesCleander stop", c.Name)
		c.wg.Done()
	}()

	return c, nil
}

// Close stops the cache.
//
// The cache may be used after it is stopped, but it is no longer cleaned.
func (c *FSCache) Close() {
	log.Debugf("cache %q: stopping", c.Name)
	close(c.stopCh)
	c.wg.Wait()
	log.Debugf("cache %q: stopped", c.Name)
}

func (c *FSCache) cleaner() {
	d := c.expire / 2
	if d < time.Minute {
		d = time.Minute
	}
	if d > time.Hour {
		d = time.Hour
	}
	forceCleanCh := time.After(d)

	c.clean()
	for {
		select {
		case <-time.After(time.Second):
			// Clean cache only on cache size overflow.
			stats := c.Stats()
			if stats.Size > c.maxSize {
				c.clean()
			}
		case <-forceCleanCh:
			// Forcibly clean cache from expired items.
			c.clean()
			forceCleanCh = time.After(d)
		case <-c.stopCh:
			return
		}
	}
}

func (c *FSCache) clean() {
	currentTime := time.Now()

	log.Debugf("cache %q: start cleaning dir %q", c.Name, c.dir)

	// Remove cached files after a graceTime from their expiration,
	// so they may be served until they are substituted with fresh files.
	expire := c.expire + c.graceTime

	// Calculate total cache size and remove expired files.
	var totalSize uint64
	var totalItems uint64
	var removedSize uint64
	var removedItems uint64
	err := walkDir(c.dir, func(fi os.FileInfo) {
		mt := fi.ModTime()
		fs := uint64(fi.Size())
		if currentTime.Sub(mt) > expire {
			fn := c.fileInfoPath(fi)
			err := os.Remove(fn)
			if err == nil {
				removedSize += fs
				removedItems++
				return
			}
			log.Errorf("cache %q: cannot remove file %q: %s", c.Name, fn, err)
			// Return skipped intentionally.
		}
		totalSize += fs
		totalItems++
	})
	if err != nil {
		log.Errorf("cache %q: %s", c.Name, err)
		return
	}

	loopsCount := 0

	// Use dedicated random generator instead of global one from math/rand,
	// since the global generator is slow due to locking.
	//
	// Seed the generator with the current time in order to randomize
	// set of files to be removed below.
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	for totalSize > c.maxSize && loopsCount < 3 {
		// Remove some files in order to reduce cache size.
		excessSize := totalSize - c.maxSize
		p := int32(float64(excessSize) / float64(totalSize) * 100)
		// Remove +10% over totalSize.
		p += 10
		err := walkDir(c.dir, func(fi os.FileInfo) {
			if rnd.Int31n(100) > p {
				return
			}

			fs := uint64(fi.Size())
			fn := c.fileInfoPath(fi)
			if err := os.Remove(fn); err != nil {
				log.Errorf("cache %q: cannot remove file %q: %s", c.Name, fn, err)
				return
			}
			removedSize += fs
			removedItems++
			totalSize -= fs
			totalItems--
		})
		if err != nil {
			log.Errorf("cache %q: %s", c.Name, err)
			return
		}

		// This should protect from infinite loop.
		loopsCount++
	}

	atomic.StoreUint64(&c.stats.Size, totalSize)
	atomic.StoreUint64(&c.stats.Items, totalItems)

	log.Debugf("cache %q: final size %d; final items %d; removed size %d; removed items %d",
		c.Name, totalSize, totalItems, removedSize, removedItems)

	log.Debugf("cache %q: finish cleaning dir %q", c.Name, c.dir)
}

// WriteTo writes cached response for the given key to rw.
//
// Returns ErrMissing if the response isn't found in the cache.
func (c *FSCache) WriteTo(rw http.ResponseWriter, key *Key) error {
	return c.writeTo(rw, key, http.StatusOK)
}

func (c *FSCache) writeTo(rw http.ResponseWriter, key *Key, statusCode int) error {
	f, err := c.get(key)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := sendResponseFromFile(rw, f, c.expire, statusCode); err != nil {
		return fmt.Errorf("cache %q: %s", c.Name, err)
	}

	return nil
}

func (c *FSCache) get(key *Key) (*os.File, error) {
	fp := c.filepath(key)

	startTime := time.Now()

again:
	f, err := os.Open(fp)
	if err != nil {
		if !os.IsNotExist(err) {
			// Unexpected error.
			return nil, fmt.Errorf("cache %q: cannot open %q: %s", c.Name, fp, err)
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
		return nil, fmt.Errorf("cache %q: cannot stat %q: %s", c.Name, fp, err)
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

func (c *FSCache) registerPendingEntry(path string) bool {
	if c.graceTime <= 0 {
		return true
	}

	c.pendingEntriesLock.Lock()
	_, exists := c.pendingEntries[path]
	if !exists {
		c.pendingEntries[path] = pendingEntry{
			deadline: time.Now().Add(c.graceTime),
		}
	}
	c.pendingEntriesLock.Unlock()
	return !exists
}

func (c *FSCache) unregisterPendingEntry(path string) {
	if c.graceTime <= 0 {
		return
	}

	c.pendingEntriesLock.Lock()
	delete(c.pendingEntries, path)
	c.pendingEntriesLock.Unlock()
}

func (c *FSCache) pendingEntriesCleaner() {
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
		for path, pe := range c.pendingEntries {
			if currentTime.After(pe.deadline) {
				delete(c.pendingEntries, path)
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

func (c *FSCache) filepath(key *Key) string {
	k := key.String()
	return filepath.Join(c.dir, k)
}

func (c *FSCache) fileInfoPath(fi os.FileInfo) string {
	return filepath.Join(c.dir, fi.Name())
}

// NewResponseWriter wraps rw into cached response writer
// that automatically caches the response under the given key.
//
// The rw must implement http.CloseNotifier.
//
// Finalize or Rollback must be called on the returned response writer
// after it is no longer needed.
//func (c *FSCache) NewResponseWriter(rw http.ResponseWriter, key *Key) (*FSResponseWriter, error) {
//	f, err := ioutil.TempFile(c.dir, "tmp")
//	if err != nil {
//		return nil, fmt.Errorf("cache %q: cannot create temporary file in %q: %s", c.Name, c.dir, err)
//	}
//	return &FSResponseWriter{
//		ResponseWriter: rw,
//
//		key: key,
//		c:   c,
//
//		tmpFile: f,
//		bw:      bufio.NewWriter(f),
//	}, nil
//}

func writeHeader(w io.Writer, s string) error {
	n := uint32(len(s))

	b := make([]byte, 0, n+4)
	b = append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	b = append(b, s...)
	_, err := w.Write(b)
	return err
}

func readHeader(r io.Reader) (string, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("cannot read header length: %s", err)
	}
	n := uint32(b[3]) | (uint32(b[2]) << 8) | (uint32(b[1]) << 16) | (uint32(b[0]) << 24)
	s := make([]byte, n)
	if _, err := io.ReadFull(r, s); err != nil {
		return "", fmt.Errorf("cannot read header value with length %d: %s", n, err)
	}
	return string(s), nil
}
