package main

import (
	"io"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
)

// statResponseWriter collects the amount of bytes written.
//
// Additionally it caches response status code.
type statResponseWriter struct {
	http.ResponseWriter

	statusCode   int
	bytesWritten prometheus.Counter
}

func (rw *statResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten.Add(float64(n))
	return n, err
}

func (rw *statResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
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

	start []byte
}

func (crc *cachedReadCloser) Read(p []byte) (int, error) {
	n, err := crc.ReadCloser.Read(p)
	if len(crc.start) < 1024 {
		crc.start = append(crc.start, p[:n]...)
		if len(crc.start) >= 1024 {
			crc.start = append(crc.start[:1024], "..."...)
		}
	}
	// Do not cache the last read operation, since it slows down
	// reading large amounts of data such as large INSERT queries.
	return n, err
}

func (crc *cachedReadCloser) String() string {
	return string(crc.start)
}