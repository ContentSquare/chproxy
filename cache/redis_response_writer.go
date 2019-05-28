package cache

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"sync/atomic"
)

// RedisResponseWriter caches the response.
//
// Commit or Rollback must be called after the response writer
// is no longer needed.
type RedisResponseWriter struct {
	http.ResponseWriter // the original response writer

	headersCaptured bool
	statusCode      int

	key *Key
	c   *RedisCache

	tmpFile *os.File      // temporary file for response streaming
	bw      *bufio.Writer // buffered writer for the temporary file
}

func (rw *RedisResponseWriter) captureHeaders() error {
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
func (rw *RedisResponseWriter) CloseNotify() <-chan bool {
	// The rw.RedisResponseWriter must implement http.CloseNotifier.
	return rw.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// WriteHeader captures response status code.
func (rw *RedisResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	// Do not call rw.RedisResponseWriter.WriteHeader here
	// It will be called explicitly in Commit / Rollback.
}

// StatusCode returns captured status code from WriteHeader.
func (rw *RedisResponseWriter) StatusCode() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

// Write writes b into rw.
func (rw *RedisResponseWriter) Write(b []byte) (int, error) {
	if err := rw.captureHeaders(); err != nil {
		return 0, err
	}
	return rw.bw.Write(b)
}

// Commit stores the response to the cache and writes it
// to the wrapped response writer.
func (rw *RedisResponseWriter) Commit() error {
	fp := rw.key.String()
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
func (rw *RedisResponseWriter) Rollback() error {
	defer rw.c.unregisterPendingEntry(rw.key.String())
	//fn := rw.tmpFile.Name()

	if err := rw.captureHeaders(); err != nil {
		rw.c.connection.Del(rw.key.String())
		return err
	}
	data, _ := rw.c.connection.Get(rw.key.String()).Bytes()
	ttl, _ := rw.c.connection.TTL(rw.key.String()).Result()
	if err := sendResponseFromRedis(rw.ResponseWriter, data, ttl, 0, rw.StatusCode()); err != nil {
		rw.c.connection.Del(rw.key.String())
		return fmt.Errorf("cache %q: %s", rw.c.Name, err)
	}

	return nil
}
