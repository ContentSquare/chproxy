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

// statReadCloser allows to store amount of read bytes into metric
type statReadCloser struct {
	io.ReadCloser
	requestBodyBytes prometheus.Counter
}

func (src *statReadCloser) Read(p []byte) (int, error) {
	n, err := src.ReadCloser.Read(p)
	src.requestBodyBytes.Add(float64(n))
	return n, err
}

// cachedReadCloser allows to read end and beginning
// of request even after body was close
type cachedReadCloser struct {
	io.ReadCloser

	mu         sync.Mutex
	start, end []byte
}

func (crc *cachedReadCloser) Read(p []byte) (int, error) {
	n, err := crc.ReadCloser.Read(p)
	crc.mu.Lock()
	if len(crc.start) == 0 {
		crc.start = p[:n]
	} else {
		crc.end = p[:n]
	}
	crc.mu.Unlock()
	return n, err
}

func (crc *cachedReadCloser) readCached() []byte {
	var b []byte
	crc.mu.Lock()
	b = append(b, crc.start...)
	if len(crc.end) > 0 {
		b = append(b, "..."...)
		b = append(b, crc.end...)
	}
	crc.mu.Unlock()
	return b
}
