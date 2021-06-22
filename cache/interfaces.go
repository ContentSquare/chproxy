package cache

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"sync"
	"time"
)

type TransactionRepository interface {
	Register(key *Key) error
	Status(key *Key) bool // todo

	Commit(r io.Reader, key *Key) error
	Rollback(r io.Reader, key *Key) error
}

type pendingEntry struct {
	deadline time.Time
}

type InMemoryTransaction struct {
	pendingEntriesLock sync.Mutex
	pendingEntries     map[*Key]pendingEntry
}

func (i *InMemoryTransaction) Commit(r io.Reader, key *Key) error {
	panic("implement me")
}

func (i *InMemoryTransaction) Rollback(r io.Reader, key *Key) error {
	panic("implement me")
}

func (i *InMemoryTransaction) Register(key *Key) error {
	panic("implement me")
}

func (i *InMemoryTransaction) Status(key *Key) bool {
	panic("implement me")
}

func (i *InMemoryTransaction) Finalize(status bool, key *Key) error {
	panic("implement me")
}

type RedisTransaction struct {
	// redisConnection
}

func (rt *RedisTransaction) Commit(r io.Reader, key *Key) error {
	panic("implement me")
}

func (rt *RedisTransaction) Rollback(r io.Reader, key *Key) error {
	panic("implement me")
}

func (rt *RedisTransaction) Register(key *Key) error {
	panic("implement me")
}

func (rt *RedisTransaction) Status(key *Key) bool {
	panic("implement me")
}

func (rt *RedisTransaction) Finalize(status bool, key *Key) error {
	panic("implement me")
}

type AsyncCache struct {
	Cache
	TransactionRepository
}

func NewAsyncCache() *AsyncCache {
	return &AsyncCache{}
}

type Cache interface {
	io.Closer

	Stats() Stats
	Get(w io.Writer, key *Key) error
	Put(r io.Reader, key *Key) error
	Name() string
	//Path(key *Key) string
}

//type CacheLookup interface {
//	TransactionRepository
//	Lookup(w io.Writer, key *Key) error
//}

//type FSCacheLookup struct {
//	TransactionRepository
//	pendingEntry map[*Key]pendingEntry
//	Cache
//}
//
//func (fs *FSCacheLookup) Register(key *Key) error {
//	return fs.TransactionRepository.Register(key)
//}
//
//func (fs *FSCacheLookup) Finalize(r io.Reader, key *Key) error {
//	return fs
//}
//
//func (fs *FSCacheLookup) Rollback(r io.Reader, key *Key) error {
//	panic("implement me")
//}
//
//func (fs *FSCacheLookup) Lookup(w io.Writer, key *Key) error {
//	if _, ok := fs.pendingEntry[key]; ok {
//		// loop for grace time
//	} else {
//		err := fs.Cache.Get(w, key)
//		if err != nil {
//
//		}
//	}
//
//}

// ClickhouseResponseWriter caches Clickhouse response.
//
// Collect response from clickhouse, capture headers and status.
type ClickhouseResponseWriter struct {
	http.ResponseWriter // the original response writer // todo

	headersCaptured bool
	statusCode      int

	tmpFile *os.File      // temporary file for response streaming
	bw      *bufio.Writer // buffered writer for the temporary file
}

func NewClickhouseResponseWriter(rw http.ResponseWriter, dir string) (*ClickhouseResponseWriter, error) {
	f, err := ioutil.TempFile(dir, "tmp")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary file in %q: %s", dir, err)
	}
	return &ClickhouseResponseWriter{
		ResponseWriter: rw,

		tmpFile: f,
		bw:      bufio.NewWriter(f),
	}, nil
}

func (rw *ClickhouseResponseWriter) GetReader() (io.Reader, error) {
	if err := rw.bw.Flush(); err != nil {
		rw.tmpFile.Close()
		fn := rw.tmpFile.Name()
		os.Remove(fn)
		return nil, fmt.Errorf("cannot flush data into %q: %s", fn, err)
	}
	return rw.tmpFile, nil
}

func (rw *ClickhouseResponseWriter) captureHeaders() error {
	if rw.headersCaptured {
		return nil
	}

	rw.headersCaptured = true
	h := rw.Header()

	ct := h.Get("Content-Type")
	if err := writeHeader(rw.bw, ct); err != nil {
		fn := rw.tmpFile.Name()
		return fmt.Errorf("cannot write Content-Type to %q: %s", fn, err) // todo
	}
	ce := h.Get("Content-Encoding")
	if err := writeHeader(rw.bw, ce); err != nil {
		//fn := rw.tmpFile.Name()
		return fmt.Errorf("cache %q: cannot write Content-Encoding to %q: %s", err) // todo
	}
	return nil
}

// CloseNotify implements http.CloseNotifier
func (rw *ClickhouseResponseWriter) CloseNotify() <-chan bool {
	// The rw.FSResponseWriter must implement http.CloseNotifier.
	return rw.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// WriteHeader captures response status code.
func (rw *ClickhouseResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	// Do not call rw.ClickhouseResponseWriter.WriteHeader here
	// It will be called explicitly in Finalize / Rollback.
}

// StatusCode returns captured status code from WriteHeader.
func (rw *ClickhouseResponseWriter) StatusCode() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

// Write writes b into rw.
func (rw *ClickhouseResponseWriter) Write(b []byte) (int, error) {
	if err := rw.captureHeaders(); err != nil {
		return 0, err
	}
	return rw.bw.Write(b)
}
