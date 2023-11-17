package cache

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/contentsquare/chproxy/log"
)

// TmpFileResponseWriter caches Clickhouse response.
// the http header are kept in memory
type TmpFileResponseWriter struct {
	http.ResponseWriter // the original response writer

	contentLength   int64
	contentType     string
	contentEncoding string
	headersCaptured bool
	statusCode      int

	tmpFile *os.File      // temporary file for response streaming
	bw      *bufio.Writer // buffered writer for the temporary file
}

func NewTmpFileResponseWriter(rw http.ResponseWriter, dir string) (*TmpFileResponseWriter, error) {
	_, ok := rw.(http.CloseNotifier)
	if !ok {
		return nil, fmt.Errorf("the response writer does not implement http.CloseNotifier")
	}

	f, err := os.CreateTemp(dir, "tmp")
	if err != nil {
		return nil, fmt.Errorf("cannot create temporary file in %q: %w", dir, err)
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
		errTmp := rw.tmpFile.Close()
		if errTmp != nil {
			log.Errorf("cannot close tmpFile: %s, error: %s", fn, errTmp)
		}
		errTmp = os.Remove(fn)
		if errTmp != nil {
			log.Errorf("cannot remove tmpFile: %s, error: %s", fn, errTmp)
		}
		return nil, fmt.Errorf("cannot flush data into %q: %w", fn, err)
	}

	return rw.tmpFile, nil
}

func (rw *TmpFileResponseWriter) Reader() (io.Reader, error) {
	f, err := rw.GetFile()
	if err != nil {
		return nil, fmt.Errorf("cannot open tmp file: %w", err)
	}
	return f, nil
}

func (rw *TmpFileResponseWriter) ResetFileOffset() error {
	data, err := rw.GetFile()
	if err != nil {
		return err
	}
	if _, err := data.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("cannot reset offset in: %w", err)
	}
	return nil
}

func (rw *TmpFileResponseWriter) captureHeaders() error {
	if rw.headersCaptured {
		return nil
	}

	rw.headersCaptured = true
	h := rw.Header()

	ct := h.Get("Content-Type")

	ce := h.Get("Content-Encoding")

	rw.contentEncoding = ce
	rw.contentType = ct
	// nb: the Content-Length http header is not set by CH so we can't get it
	return nil
}

func (rw *TmpFileResponseWriter) GetCapturedContentType() string {
	return rw.contentType
}

func (rw *TmpFileResponseWriter) GetCapturedContentLength() (int64, error) {
	if rw.contentLength == 0 {
		// Determine Content-Length looking at the file
		data, err := rw.GetFile()
		if err != nil {
			return 0, fmt.Errorf("GetCapturedContentLength: cannot open tmp file: %w", err)
		}

		end, err := data.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, fmt.Errorf("GetCapturedContentLength: cannot determine the last position in: %w", err)
		}
		if err := rw.ResetFileOffset(); err != nil {
			return 0, err
		}
		return end - 0, nil
	}
	return rw.contentLength, nil
}

func (rw *TmpFileResponseWriter) GetCapturedContentEncoding() string {
	return rw.contentEncoding
}

// CloseNotify implements http.CloseNotifier
func (rw *TmpFileResponseWriter) CloseNotify() <-chan bool {
	// nolint:forcetypeassert // it is guaranteed by NewTmpFileResponseWriter
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
