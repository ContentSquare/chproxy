package main

import (
	"io"
	"net/http"
	"sync"
	"github.com/prometheus/client_golang/prometheus"
)

// statResponseWriter allows to cache statusCode after proxying
type statResponseWriter struct {
	http.ResponseWriter
	statusCode        int
	responseBodyBytes prometheus.Counter
}

func (rw *statResponseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.responseBodyBytes.Add(float64(n))
	return n, err
}

func (rw *statResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// readCloser allows to read requestBody twice
// which is important for proper error logging
// also provides body-bytes metric
type statReadCloser struct {
	io.ReadCloser
	requestBodyBytes prometheus.Counter

	mu         sync.Mutex
	start, end []byte
}

func (src *statReadCloser) readCached() []byte {
	src.mu.Lock()
	b := make([]byte, len(src.start)+len(src.end))
	b = append(b, src.start...)
	b = append(b, src.end...)
	src.mu.Unlock()
	return b
}

func (src *statReadCloser) Read(p []byte) (int, error) {
	n, err := src.ReadCloser.Read(p)
	src.mu.Lock()
	if len(src.start) == 0 {
		src.start = p[:n]
	} else {
		src.end = p[:n]
	}
	src.mu.Unlock()
	src.requestBodyBytes.Add(float64(n))
	return n, err
}
