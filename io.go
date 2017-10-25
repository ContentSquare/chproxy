package main

import (
	"io"
	"io/ioutil"
	"net/http"

	"github.com/Vertamedia/chproxy/cache"
	"github.com/prometheus/client_golang/prometheus"
)

type cachedResponseWriter struct {
	cc  *cache.Controller
	key string

	statResponseWriter
}

func (crw *cachedResponseWriter) Write(b []byte) (int, error) {
	if crw.statusCode == http.StatusOK {
		go crw.cc.Store(crw.key, b)
	}
	return crw.ResponseWriter.Write(b)
}

// cached writer supposed to intercept headers set
type statResponseWriter struct {
	http.ResponseWriter
	statusCode        int
	responseBodyBytes prometheus.Counter
}

func (rw *statResponseWriter) Write(b []byte) (int, error) {
	rw.statusCode = http.StatusOK
	n, err := rw.ResponseWriter.Write(b)
	rw.responseBodyBytes.Add(float64(n))
	return n, err
}

func (rw *statResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

type readCloser struct {
	io.ReadCloser
	requestBodyBytes prometheus.Counter
	peekedBytes      []byte
}

func (rc *readCloser) Read(p []byte) (int, error) {
	// first return data from peekedBytes, then from the original reader
	n := copy(p, rc.peekedBytes)
	if n > 0 {
		rc.requestBodyBytes.Add(float64(n))
		rc.peekedBytes = rc.peekedBytes[n:]
		return n, nil
	}
	n, err := rc.ReadCloser.Read(p)
	rc.requestBodyBytes.Add(float64(n))
	return n, err
}

func (rc *readCloser) readFirstN(n int64) ([]byte, error) {
	b, err := ioutil.ReadAll(io.LimitReader(rc, n))
	if err != nil {
		return b, err
	}
	rc.peekedBytes = b
	return b, nil
}
