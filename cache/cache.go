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

// cacheVersion must be increased with each backwads-incompatible change
// in the cache storage.
const cacheVersion = 2

// Cache represents a file cache.
type Cache struct {
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

	// AcceptEncoding must contain 'Accept-Encoding' request header value.
	AcceptEncoding string

	// DefaultFormat must contain `default_format` query arg.
	DefaultFormat string

	// Database must contain `database` query arg.
	Database string

	// Compress must contain `compress` query arg.
	Compress string

	// EnableHTTPCompression must contain `enable_http_compression` query arg.
	EnableHTTPCompression string

	// Namespace is an optional cache namespace.
	Namespace string

	// MaxResultRows must contain `max_result_rows` query arg
	MaxResultRows string

	// Extremes must contain `extremes` query arg
	Extremes string

	// ResultOverflowMode must contain `result_overflow_mode` query arg
	ResultOverflowMode string

	// UserParamsHash must contain hashed value of users params
	UserParamsHash uint32
}

// String returns string representation of the key.
func (k *Key) String() string {
	s := fmt.Sprintf("V%d; Query=%q; AcceptEncoding=%q; DefaultFormat=%q; Database=%q; Compress=%q; EnableHTTPCompression=%q; Namespace=%q; MaxResultRows=%q; Extremes=%q; ResultOverflowMode=%q; UserParams=%d",
		cacheVersion, k.Query, k.AcceptEncoding, k.DefaultFormat, k.Database, k.Compress, k.EnableHTTPCompression, k.Namespace,
		k.MaxResultRows, k.Extremes, k.ResultOverflowMode, k.UserParamsHash)
	h := sha256.Sum256([]byte(s))

	// The first 16 bytes of the hash should be enough
	// for collision prevention :)
	return hex.EncodeToString(h[:16])
}

// This regexp must match Key.String output
var cachefileRegexp = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Stats returns cache stats.
//
// The returned stats is approximate.
func (c *Cache) Stats() Stats {
	var s Stats
	s.Size = atomic.LoadUint64(&c.stats.Size)
	s.Items = atomic.LoadUint64(&c.stats.Items)
	return s
}

// New returns new cache for the given cfg.
func New(cfg config.Cache) (*Cache, error) {
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

	c := &Cache{
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
func (c *Cache) Close() {
	log.Debugf("cache %q: stopping", c.Name)
	close(c.stopCh)
	c.wg.Wait()
	log.Debugf("cache %q: stopped", c.Name)
}

func (c *Cache) cleaner() {
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

func (c *Cache) clean() {
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
	return c.writeTo(rw, key, http.StatusOK)
}

func (c *Cache) writeTo(rw http.ResponseWriter, key *Key, statusCode int) error {
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

func (c *Cache) get(key *Key) (*os.File, error) {
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

func (c *Cache) registerPendingEntry(path string) bool {
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

func (c *Cache) unregisterPendingEntry(path string) {
	if c.graceTime <= 0 {
		return
	}

	c.pendingEntriesLock.Lock()
	delete(c.pendingEntries, path)
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

func (c *Cache) filepath(key *Key) string {
	k := key.String()
	return filepath.Join(c.dir, k)
}

func (c *Cache) fileInfoPath(fi os.FileInfo) string {
	return filepath.Join(c.dir, fi.Name())
}

// NewResponseWriter wraps rw into cached response writer
// that automatically caches the response under the given key.
//
// The rw must implement http.CloseNotifier.
//
// Commit or Rollback must be called on the returned response writer
// after it is no longer needed.
func (c *Cache) NewResponseWriter(rw http.ResponseWriter, key *Key) (*ResponseWriter, error) {
	f, err := ioutil.TempFile(c.dir, "tmp")
	if err != nil {
		return nil, fmt.Errorf("cache %q: cannot create temporary file in %q: %s", c.Name, c.dir, err)
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

	headersCaptured bool
	statusCode      int

	key *Key
	c   *Cache

	tmpFile *os.File      // temporary file for response streaming
	bw      *bufio.Writer // buffered writer for the temporary file
}

func (rw *ResponseWriter) captureHeaders() error {
	if rw.headersCaptured {
		return nil
	}

	rw.headersCaptured = true
	h := rw.Header()

	ct := h.Get("Content-Type")
	if err := writeHeader(rw.bw, ct); err != nil {
		fn := rw.tmpFile.Name()
		return fmt.Errorf("cache %q: cannot write Content-Type to %q: %s", rw.c.Name, fn, err)
	}
	ce := h.Get("Content-Encoding")
	if err := writeHeader(rw.bw, ce); err != nil {
		fn := rw.tmpFile.Name()
		return fmt.Errorf("cache %q: cannot write Content-Encoding to %q: %s", rw.c.Name, fn, err)
	}
	return nil
}

// CloseNotify implements http.CloseNotifier
func (rw *ResponseWriter) CloseNotify() <-chan bool {
	// The rw.ResponseWriter must implement http.CloseNotifier.
	return rw.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// WriteHeader captures response status code.
func (rw *ResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	// Do not call rw.ResponseWriter.WriteHeader here
	// It will be called explicitly in Commit / Rollback.
}

// StatusCode returns captured status code from WriteHeader.
func (rw *ResponseWriter) StatusCode() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

// Write writes b into rw.
func (rw *ResponseWriter) Write(b []byte) (int, error) {
	if err := rw.captureHeaders(); err != nil {
		return 0, err
	}
	return rw.bw.Write(b)
}

// Commit stores the response to the cache and writes it
// to the wrapped response writer.
func (rw *ResponseWriter) Commit() error {
	fp := rw.c.filepath(rw.key)
	defer rw.c.unregisterPendingEntry(fp)
	fn := rw.tmpFile.Name()

	if err := rw.captureHeaders(); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return err
	}

	if err := rw.bw.Flush(); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot flush data into %q: %s", rw.c.Name, fn, err)
	}

	// Update cache stats.
	fi, err := rw.tmpFile.Stat()
	if err != nil {
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot stat %q: %s", rw.c.Name, fn, err)
	}
	fs := uint64(fi.Size())
	atomic.AddUint64(&rw.c.stats.Size, fs)
	atomic.AddUint64(&rw.c.stats.Items, 1)

	if err := rw.tmpFile.Close(); err != nil {
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot close %q: %s", rw.c.Name, fn, err)
	}

	if err := os.Rename(fn, fp); err != nil {
		return fmt.Errorf("cache %q: cannot rename %q to %q: %s", rw.c.Name, fn, fp, err)
	}

	return rw.c.writeTo(rw.ResponseWriter, rw.key, rw.StatusCode())
}

// Rollback writes the response to the wrapped response writer and discards
// it from the cache.
func (rw *ResponseWriter) Rollback() error {
	fp := rw.c.filepath(rw.key)
	defer rw.c.unregisterPendingEntry(fp)
	fn := rw.tmpFile.Name()

	if err := rw.captureHeaders(); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return err
	}

	if err := rw.bw.Flush(); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot flush data into %q: %s", rw.c.Name, fn, err)
	}

	if _, err := rw.tmpFile.Seek(0, io.SeekStart); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot seek to the beginning of %q: %s", rw.c.Name, fn, err)
	}

	if err := sendResponseFromFile(rw.ResponseWriter, rw.tmpFile, 0, rw.StatusCode()); err != nil {
		rw.tmpFile.Close()
		os.Remove(fn)
		return fmt.Errorf("cache %q: %s", rw.c.Name, err)
	}

	if err := rw.tmpFile.Close(); err != nil {
		os.Remove(fn)
		return fmt.Errorf("cache %q: cannot close %q: %s", rw.c.Name, fn, err)
	}
	if err := os.Remove(fn); err != nil {
		return fmt.Errorf("cache %q: cannot remove %q: %s", rw.c.Name, fn, err)
	}
	return nil
}

// sendResponseFromFile sends response to rw from f.
//
// Sets 'Cache-Control: max-age' header if expire > 0.
// Sets the given response status code.
func sendResponseFromFile(rw http.ResponseWriter, f *os.File, expire time.Duration, statusCode int) error {
	h := rw.Header()

	ct, err := readHeader(f)
	if err != nil {
		return fmt.Errorf("cannot read Content-Type from %q: %s", f.Name(), err)
	}
	if len(ct) > 0 {
		h.Set("Content-Type", ct)
	}
	ce, err := readHeader(f)
	if err != nil {
		return fmt.Errorf("cannot read Content-Encoding from %q: %s", f.Name(), err)
	}
	if len(ce) > 0 {
		h.Set("Content-Encoding", ce)
	}

	// Determine Content-Length
	off, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("cannot determine the current position in %q: %s", f.Name(), err)
	}
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat %q: %s", f.Name(), err)
	}
	fs := fi.Size()
	cl := fs - off
	h.Set("Content-Length", fmt.Sprintf("%d", cl))

	// Set 'Cache-Control: max-age' on non-temporary file
	if expire > 0 {
		mt := fi.ModTime()
		age := time.Since(mt)
		left := expire - age
		if left > 0 {
			leftSeconds := uint(left / time.Second)
			h.Set("Cache-Control", fmt.Sprintf("max-age=%d", leftSeconds))
		}
	}

	rw.WriteHeader(statusCode)
	if _, err := io.Copy(rw, f); err != nil {
		return fmt.Errorf("cannot send %q to client: %s", f.Name(), err)
	}
	return nil
}

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
