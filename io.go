package main

import (
	"io"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"bytes"
	"fmt"
)

type responseRecorder struct {
	body *bytes.Buffer

	http.ResponseWriter
}

func newRecorder(rw http.ResponseWriter) *responseRecorder {
	return &responseRecorder{
		ResponseWriter: rw,
		body: new(bytes.Buffer),
	}
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	return rr.body.Write(b)
}

func (rr responseRecorder) result() []byte {
	return rr.body.Bytes()
}

func (rr responseRecorder) write() (int, error) {
	return rr.ResponseWriter.Write(rr.body.Bytes())
}

// cached writer supposed to intercept headers set
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

type readCloser struct {
	io.ReadCloser
	requestBodyBytes prometheus.Counter

	readBytes      []byte
	cachedBytes	[]byte

}

func (rc *readCloser) Read(p []byte) (int, error) {
	n := copy(p, rc.readBytes)
	if n > 0 {
		rc.requestBodyBytes.Add(float64(n))
		rc.readBytes = rc.readBytes[n:]
		return n, nil
	}
	n, err := rc.ReadCloser.Read(p)
	if len(rc.cachedBytes) == 0 {
		rc.cachedBytes = p[:n]
		rc.readBytes = p[:n]
	}
	rc.requestBodyBytes.Add(float64(n))
	return n, err
}