package cache

import (
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/contentsquare/chproxy/config"
	"github.com/contentsquare/chproxy/log"
)

var cachefileRegexp = regexp.MustCompile(`^[0-9a-f]{32}$`)

// fileSystemCache represents a file cache.
type fileSystemCache struct {
	// name is cache name.
	name string

	dir     string
	maxSize uint64
	expire  time.Duration
	grace   time.Duration
	stats   Stats

	wg     sync.WaitGroup
	stopCh chan struct{}
}

// newFilesSystemCache returns new cache for the given cfg.
func newFilesSystemCache(cfg config.Cache, graceTime time.Duration) (*fileSystemCache, error) {
	if len(cfg.FileSystem.Dir) == 0 {
		return nil, fmt.Errorf("`dir` cannot be empty")
	}
	if cfg.FileSystem.MaxSize <= 0 {
		return nil, fmt.Errorf("`max_size` must be positive")
	}
	if cfg.Expire <= 0 {
		return nil, fmt.Errorf("`expire` must be positive")
	}

	c := &fileSystemCache{
		name: cfg.Name,

		dir:     cfg.FileSystem.Dir,
		maxSize: uint64(cfg.FileSystem.MaxSize),
		expire:  time.Duration(cfg.Expire),
		grace:   graceTime,
		stopCh:  make(chan struct{}),
	}

	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create %q: %w", c.dir, err)
	}

	c.wg.Add(1)
	go func() {
		log.Debugf("cache %q: cleaner start", c.Name())
		c.cleaner()
		log.Debugf("cache %q: cleaner stop", c.Name())
		c.wg.Done()
	}()

	return c, nil
}

func (f *fileSystemCache) Name() string {
	return f.name
}

func (f *fileSystemCache) Close() error {
	log.Debugf("cache %q: stopping", f.Name())
	close(f.stopCh)
	f.wg.Wait()
	log.Debugf("cache %q: stopped", f.Name())
	return nil
}

func (f *fileSystemCache) Stats() Stats {
	var s Stats
	s.Size = atomic.LoadUint64(&f.stats.Size)
	s.Items = atomic.LoadUint64(&f.stats.Items)
	return s
}

func (f *fileSystemCache) Get(key *Key) (*CachedData, error) {
	fp := key.filePath(f.dir)
	file, err := os.Open(fp)
	if err != nil {
		return nil, ErrMissing
	}

	// the file will be closed once it's read as an io.ReaderCloser
	// This  ReaderCloser is stored in the returned CachedData
	fi, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("cache %q: cannot stat %q: %w", f.Name(), fp, err)
	}
	mt := fi.ModTime()
	age := time.Since(mt)
	if age > f.expire {
		// check if file exceeded expiration time + grace time
		if age > f.expire+f.grace {
			file.Close()
			return nil, ErrMissing
		}
		// Serve expired file in the hope it will be substituted
		// with the fresh file during deadline.
	}

	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read file content from %q: %w", f.Name(), err)
	}

	metadata, err := decodeHeader(file)
	if err != nil {
		return nil, err
	}

	value := &CachedData{
		ContentMetadata: *metadata,
		Data:            file,
		Ttl:             f.expire - age,
	}

	return value, nil
}

// decodeHeader decodes header from raw byte stream. Data is encoded as follows:
// length(contentType)|contentType|length(contentEncoding)|contentEncoding|length(contentLength)|contentLength|cachedData
func decodeHeader(reader io.Reader) (*ContentMetadata, error) {
	contentType, err := readHeader(reader)
	if err != nil {
		return nil, fmt.Errorf("cannot read Content-Type from provided reader: %w", err)
	}

	contentEncoding, err := readHeader(reader)
	if err != nil {
		return nil, fmt.Errorf("cannot read Content-Encoding from provided reader: %w", err)
	}

	contentLengthStr, err := readHeader(reader)
	if err != nil {
		return nil, fmt.Errorf("cannot read Content-Encoding from provided reader: %w", err)
	}

	contentLength, err := strconv.Atoi(contentLengthStr)
	if err != nil {
		log.Errorf("found corrupted content length %s", err)
		contentLength = 0
	}

	return &ContentMetadata{
		Length:   int64(contentLength),
		Type:     contentType,
		Encoding: contentEncoding,
	}, nil
}

func (f *fileSystemCache) Put(r io.Reader, contentMetadata ContentMetadata, key *Key) (time.Duration, error) {
	fp := key.filePath(f.dir)
	file, err := os.Create(fp)

	if err != nil {
		return 0, fmt.Errorf("cache %q: cannot create file: %s : %w", f.Name(), key, err)
	}

	if err := writeHeader(file, contentMetadata.Type); err != nil {
		fn := file.Name()
		return 0, fmt.Errorf("cannot write Content-Type to %q: %w", fn, err)
	}

	if err := writeHeader(file, contentMetadata.Encoding); err != nil {
		fn := file.Name()
		return 0, fmt.Errorf("cannot write Content-Encoding to %q: %w", fn, err)
	}

	if err := writeHeader(file, fmt.Sprintf("%d", contentMetadata.Length)); err != nil {
		fn := file.Name()
		return 0, fmt.Errorf("cannot write Content-Encoding to %q: %w", fn, err)
	}

	cnt, err := io.Copy(file, r)
	if err != nil {
		return 0, fmt.Errorf("cache %q: cannot write results to file: %s : %w", f.Name(), key, err)
	}

	atomic.AddUint64(&f.stats.Size, uint64(cnt))
	atomic.AddUint64(&f.stats.Items, 1)
	return f.expire, nil
}

func (f *fileSystemCache) cleaner() {
	d := f.expire / 2
	if d < time.Minute {
		d = time.Minute
	}
	if d > time.Hour {
		d = time.Hour
	}
	forceCleanCh := time.After(d)

	f.clean()
	for {
		select {
		case <-time.After(time.Second):
			// Clean cache only on cache size overflow.
			stats := f.Stats()
			if stats.Size > f.maxSize {
				f.clean()
			}
		case <-forceCleanCh:
			// Forcibly clean cache from expired items.
			f.clean()
			forceCleanCh = time.After(d)
		case <-f.stopCh:
			return
		}
	}
}

func (f *fileSystemCache) fileInfoPath(fi os.FileInfo) string {
	return filepath.Join(f.dir, fi.Name())
}

func (f *fileSystemCache) clean() {
	currentTime := time.Now()

	log.Debugf("cache %q: start cleaning dir %q", f.Name(), f.dir)

	// Remove cached files after a deadline from their expiration,
	// so they may be served until they are substituted with fresh files.
	expire := f.expire + f.grace

	// Calculate total cache size and remove expired files.
	var totalSize uint64
	var totalItems uint64
	var removedSize uint64
	var removedItems uint64
	err := walkDir(f.dir, func(fi os.FileInfo) {
		mt := fi.ModTime()
		fs := uint64(fi.Size())
		if currentTime.Sub(mt) > expire {
			fn := f.fileInfoPath(fi)
			err := os.Remove(fn)
			if err == nil {
				removedSize += fs
				removedItems++
				return
			}
			log.Errorf("cache %q: cannot remove file %q: %s", f.Name(), fn, err)
			// Return skipped intentionally.
		}
		totalSize += fs
		totalItems++
	})
	if err != nil {
		log.Errorf("cache %q: %s", f.Name(), err)
		return
	}

	loopsCount := 0

	// Use dedicated random generator instead of global one from math/rand,
	// since the global generator is slow due to locking.
	//
	// Seed the generator with the current time in order to randomize
	// set of files to be removed below.
	// nolint:gosec // not security sensitve, only used internally.
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	for totalSize > f.maxSize && loopsCount < 3 {
		// Remove some files in order to reduce cache size.
		excessSize := totalSize - f.maxSize
		p := int32(float64(excessSize) / float64(totalSize) * 100)
		// Remove +10% over totalSize.
		p += 10
		err := walkDir(f.dir, func(fi os.FileInfo) {
			if rnd.Int31n(100) > p {
				return
			}

			fs := uint64(fi.Size())
			fn := f.fileInfoPath(fi)
			if err := os.Remove(fn); err != nil {
				log.Errorf("cache %q: cannot remove file %q: %s", f.Name(), fn, err)
				return
			}
			removedSize += fs
			removedItems++
			totalSize -= fs
			totalItems--
		})
		if err != nil {
			log.Errorf("cache %q: %s", f.Name(), err)
			return
		}

		// This should protect from infinite loop.
		loopsCount++
	}

	atomic.StoreUint64(&f.stats.Size, totalSize)
	atomic.StoreUint64(&f.stats.Items, totalItems)

	log.Debugf("cache %q: final size %d; final items %d; removed size %d; removed items %d",
		f.Name(), totalSize, totalItems, removedSize, removedItems)

	log.Debugf("cache %q: finish cleaning dir %q", f.Name(), f.dir)
}

// writeHeader encodes headers in little endian
func writeHeader(w io.Writer, s string) error {
	n := uint32(len(s))

	b := make([]byte, 0, n+4)
	b = append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	b = append(b, s...)
	_, err := w.Write(b)
	return err
}

// readHeader decodes headers to big endian
func readHeader(r io.Reader) (string, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("cannot read header length: %w", err)
	}
	n := uint32(b[3]) | (uint32(b[2]) << 8) | (uint32(b[1]) << 16) | (uint32(b[0]) << 24)
	s := make([]byte, n)
	if _, err := io.ReadFull(r, s); err != nil {
		return "", fmt.Errorf("cannot read header value with length %d: %w", n, err)
	}
	return string(s), nil
}
