package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"io"
	"net/http"
	"sync"
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

// readCloser allows to read end and beginning
// of request even after body was close
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
	if len(src.end) > 0 {
		b = append(b, "..."...)
		b = append(b, src.end...)
	}
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
