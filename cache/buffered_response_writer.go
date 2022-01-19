package cache

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
)

// BufferedResponseWriter caches Clickhouse response.
//
// Collect response to the buffer, capture headers and status.
type BufferedResponseWriter struct {
	http.ResponseWriter // the original response writer

	contentLength   int64
	contentType     string
	contentEncoding string
	headersCaptured bool
	statusCode      int
	buffer          *bytes.Buffer // buffer of clickhouse raw response
}

func NewBufferedResponseWriter(rw http.ResponseWriter) *BufferedResponseWriter {
	return &BufferedResponseWriter{
		ResponseWriter: rw,
		buffer:         &bytes.Buffer{},
	}
}

func (rw *BufferedResponseWriter) Reader() io.Reader {
	return rw.buffer
}

func (rw *BufferedResponseWriter) GetCapturedContentType() string {
	return rw.contentType
}

func (rw *BufferedResponseWriter) GetCapturedContentLength() int64 {
	if rw.contentLength == 0 {
		rw.contentLength = int64(rw.buffer.Len())
	}
	return rw.contentLength
}

func (rw *BufferedResponseWriter) GetCapturedContentEncoding() string {
	return rw.contentEncoding
}

func (rw *BufferedResponseWriter) captureHeaders() error {
	if rw.headersCaptured {
		return nil
	}

	rw.headersCaptured = true
	h := rw.Header()
	ct := h.Get("Content-Type")
	cl, err := strconv.Atoi(h.Get("Content-Length"))
	if err != nil {
		cl = 0
	}
	ce := h.Get("Content-Encoding")
	rw.contentEncoding = ce
	rw.contentType = ct
	rw.contentLength = int64(cl)

	return nil
}

// CloseNotify implements http.CloseNotifier
func (rw *BufferedResponseWriter) CloseNotify() <-chan bool {
	return rw.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// WriteHeader captures response status code.
func (rw *BufferedResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
}

// StatusCode returns captured status code from WriteHeader.
func (rw *BufferedResponseWriter) StatusCode() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

// Write writes b into rw.
func (rw *BufferedResponseWriter) Write(b []byte) (int, error) {
	if err := rw.captureHeaders(); err != nil {
		return 0, err
	}
	return rw.buffer.Write(b)
}
