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
