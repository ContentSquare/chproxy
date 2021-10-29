package cache

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// TODO MOVE
// SendResponseFromReader sends response to rw from data.
// Data represents data that was pushed to cache.
// The stream of data looks like the following :
// length(contentType)|contentType|length(contentEncoding)|contentEncoding|length(contentLength)|contentLength|cachedData
//
// Sets 'Cache-Control: max-age' header if expire > 0.
// Sets the given response status code.
func SendResponseFromReader(rw http.ResponseWriter, data io.ReadSeeker, ttl time.Duration, statusCode int) error {
	if _, err := data.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("sendRespFromFile: cannot seek to the beginning: %s", err)
	}

	h := rw.Header()
	ct, err := readHeader(data)
	if err != nil {
		return fmt.Errorf("cannot read Content-Type from provided reader: %s", err)
	}
	if len(ct) > 0 {
		h.Set("Content-Type", ct)
	}
	ce, err := readHeader(data)
	if err != nil {
		return fmt.Errorf("cannot read Content-Encoding from provided reader: %s", err)
	}
	if len(ce) > 0 {
		h.Set("Content-Encoding", ce)
	}

	// Determine Content-Length
	start, err := data.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("cannot determine the current position in: %s", err)
	}

	end, err := data.Seek(0, io.SeekEnd)
	if err != nil {
		return fmt.Errorf("cannot determine the current position in: %s", err)
	}

	// Determine Content-Length
	_, err = data.Seek(start, io.SeekStart)
	if err != nil {
		return fmt.Errorf("cannot determine the current position in: %s", err)
	}

	cl := end - start
	h.Set("Content-Length", fmt.Sprintf("%d", cl))

	// set CacheControl max-age
	if ttl > 0 {
		expireSeconds := uint(ttl / time.Second)
		h.Set("Cache-Control", fmt.Sprintf("max-age=%d", expireSeconds))
	}

	rw.WriteHeader(statusCode)

	if _, err := io.Copy(rw, data); err != nil {
		return fmt.Errorf("cannot send response to client: %s", err)
	}

	return nil
}

// walkDir calls f on all the cache files in the given dir.
func walkDir(dir string, f func(fi os.FileInfo)) error {
	// Do not use filepath.Walk, since it is inefficient
	// for large number of files.
	// See https://golang.org/pkg/path/filepath/#Walk .
	fd, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("cannot open %q: %s", dir, err)
	}
	defer fd.Close()

	for {
		fis, err := fd.Readdir(1024)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("cannot read files in %q: %s", dir, err)
		}
		for _, fi := range fis {
			if fi.IsDir() {
				// Skip subdirectories
				continue
			}
			fn := fi.Name()
			if !cachefileRegexp.MatchString(fn) {
				// Skip invalid filenames
				continue
			}
			f(fi)
		}
	}
}
