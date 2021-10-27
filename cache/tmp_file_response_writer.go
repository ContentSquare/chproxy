package cache

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
)

// TmpFileResponseWriter caches Clickhouse response.
//
// Collect response to tmp file, capture headers and status.
type TmpFileResponseWriter struct {
	http.ResponseWriter // the original response writer

	headersCaptured bool
	statusCode      int

	tmpFile *os.File      // temporary file for response streaming
	bw      *bufio.Writer // buffered writer for the temporary file
}

func NewTmpFileResponseWriter(rw http.ResponseWriter, dir string) (*TmpFileResponseWriter, error) {
	f, err := ioutil.TempFile(dir, "tmp")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary file in %q: %s", dir, err)
	}
	return &TmpFileResponseWriter{
		ResponseWriter: rw,

		tmpFile: f,
		bw:      bufio.NewWriter(f),
	}, nil
}

func (rw *TmpFileResponseWriter) Close() error {
	rw.tmpFile.Close()
	return os.Remove(rw.tmpFile.Name())
}

func (rw *TmpFileResponseWriter) GetFile() (*os.File, error) {
	if err := rw.bw.Flush(); err != nil {
		fn := rw.tmpFile.Name()
		err = rw.tmpFile.Close()
		err = os.Remove(fn)
		return nil, fmt.Errorf("cannot flush data into %q: %s", fn, err)
	}
	return rw.tmpFile, nil
}

func (rw *TmpFileResponseWriter) captureHeaders() error {
	if rw.headersCaptured {
		return nil
	}

	rw.headersCaptured = true
	h := rw.Header()

	ct := h.Get("Content-Type")
	if err := writeHeader(rw.bw, ct); err != nil {
		fn := rw.tmpFile.Name()
		return fmt.Errorf("cannot write Content-Type to %q: %s", fn, err)
	}
	ce := h.Get("Content-Encoding")
	if err := writeHeader(rw.bw, ce); err != nil {
		return fmt.Errorf("tmp_file_resp_writer: cannot write Content-Encoding to %q: %s", rw.tmpFile.Name(), err)
	}
	return nil
}

// CloseNotify implements http.CloseNotifier
func (rw *TmpFileResponseWriter) CloseNotify() <-chan bool {
	// The rw.FSResponseWriter must implement http.CloseNotifier.
	return rw.ResponseWriter.(http.CloseNotifier).CloseNotify()
}

// WriteHeader captures response status code.
func (rw *TmpFileResponseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	// Do not call rw.ClickhouseResponseWriter.WriteHeader here
	// It will be called explicitly in Finalize / Unregister.
}

// StatusCode returns captured status code from WriteHeader.
func (rw *TmpFileResponseWriter) StatusCode() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

// Write writes b into rw.
func (rw *TmpFileResponseWriter) Write(b []byte) (int, error) {
	if err := rw.captureHeaders(); err != nil {
		return 0, err
	}
	return rw.bw.Write(b)
}

// writeHeader encodes headers in little endian
func writeHeader(w io.Writer, s string) error {
	n := uint32(len(s))

	b := make([]byte, 0, n+4)
	b = append(b, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	b = append(b, s...)
	_, err := w.Write(b)
	return err
}

// readHeader decodes headers to big endian
func readHeader(r io.Reader) (string, error) {
	b := make([]byte, 4)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", fmt.Errorf("cannot read header length: %s", err)
	}
	n := uint32(b[3]) | (uint32(b[2]) << 8) | (uint32(b[1]) << 16) | (uint32(b[0]) << 24)
	s := make([]byte, n)
	if _, err := io.ReadFull(r, s); err != nil {
		return "", fmt.Errorf("cannot read header value with length %d: %s", n, err)
	}
	return string(s), nil
}
