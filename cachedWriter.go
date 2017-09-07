package main

import (
	"bytes"
	"net/http"
	"sync"
)

func newCachedWriter(w http.ResponseWriter) *cachedWriter {
	return &cachedWriter{
		w: w,
	}
}

type cachedWriter struct {
	w http.ResponseWriter

	mu          sync.Mutex
	wroteHeader bool
	code        int
	wbuf        bytes.Buffer
}

func (cw *cachedWriter) Header() http.Header { return cw.w.Header() }

func (cw *cachedWriter) Write(p []byte) (int, error) {
	cw.mu.Lock()
	defer cw.mu.Unlock()

	if !cw.wroteHeader {
		cw.writeHeader(http.StatusOK)
	}

	if cw.code != http.StatusOK {
		return cw.wbuf.Write(p)
	}

	return cw.w.Write(p)
}

func (cw *cachedWriter) WriteHeader(code int) {
	cw.mu.Lock()
	if !cw.wroteHeader {
		cw.writeHeader(code)
	}
	cw.mu.Unlock()
}

func (cw *cachedWriter) writeHeader(code int) {
	cw.wroteHeader = true
	cw.code = code
	cw.w.WriteHeader(code)
}

func (cw *cachedWriter) Status() int {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.code
}

func (cw *cachedWriter) Bytes() []byte {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	return cw.wbuf.Bytes()
}
