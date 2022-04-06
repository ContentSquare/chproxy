package main

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/contentsquare/chproxy/cache"

	"github.com/prometheus/client_golang/prometheus"
)

// statResponseWriter collects the amount of bytes written.
//
// The wrapped ResponseWriter must implement http.CloseNotifier.
//
// Additionally it caches response status code.
type statResponseWriter struct {
	http.ResponseWriter

	statusCode int
	// wroteHeader tells whether the header's been written to
	// the original ResponseWriter
	wroteHeader bool

	bytesWritten prometheus.Counter
}

func RespondWithData(rw http.ResponseWriter, data io.Reader, metadata cache.ContentMetadata, ttl time.Duration, statusCode int) error {
	h := rw.Header()
	if len(metadata.Type) > 0 {
		h.Set("Content-Type", metadata.Type)
	}

	if len(metadata.Encoding) > 0 {
		h.Set("Content-Encoding", metadata.Encoding)
	}

	h.Set("Content-Length", fmt.Sprintf("%d", metadata.Length))
	if ttl > 0 {
		expireSeconds := uint(ttl / time.Second)
		h.Set("Cache-Control", fmt.Sprintf("max-age=%d", expireSeconds))
	}
	rw.WriteHeader(statusCode)

	if _, err := io.Copy(rw, data); err != nil {
		return fmt.Errorf("cannot send response to client: %w", err)
	}

	return nil
}

func RespondWithoutData(rw http.ResponseWriter) error {
	_, err := rw.Write([]byte{})
	return err
}

func (rw *statResponseWriter) Write(b []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	if !rw.wroteHeader {
		rw.ResponseWriter.WriteHeader(rw.statusCode)
		rw.wroteHeader = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten.Add(float64(n))

	return n, err
}

func (rw *statResponseWriter) WriteHeader(statusCode int) {
	// cache statusCode to keep the opportunity to change it in further
	rw.statusCode = statusCode
}

// CloseNotify implements http.CloseNotifier
func (rw *statResponseWriter) CloseNotify() <-chan bool {
	// The rw.ResponseWriter must implement http.CloseNotifier
	return rw.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// statReadCloser collects the amount of bytes read.
type statReadCloser struct {
	io.ReadCloser

	bytesRead prometheus.Counter
}

func (src *statReadCloser) Read(p []byte) (int, error) {
	n, err := src.ReadCloser.Read(p)
	src.bytesRead.Add(float64(n))
	return n, err
}

// cachedReadCloser caches the first 1Kb form the wrapped ReadCloser.
type cachedReadCloser struct {
	io.ReadCloser

	// bLock protects b from concurrent access when Read and String
	// are called from concurrent goroutines.
	bLock sync.Mutex

	// b holds up to 1Kb of the initial data read from ReadCloser.
	b []byte
}

func (crc *cachedReadCloser) Read(p []byte) (int, error) {
	n, err := crc.ReadCloser.Read(p)

	crc.bLock.Lock()
	if len(crc.b) < 1024 {
		crc.b = append(crc.b, p[:n]...)
		if len(crc.b) >= 1024 {
			crc.b = append(crc.b[:1024], "..."...)
		}
	}
	crc.bLock.Unlock()

	// Do not cache the last read operation, since it slows down
	// reading large amounts of data such as large INSERT queries.
	return n, err
}

func (crc *cachedReadCloser) String() string {
	crc.bLock.Lock()
	s := string(crc.b)
	crc.bLock.Unlock()
	return s
}
