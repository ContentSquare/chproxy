package cache

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// SendResponseFromFile sends response to rw from f.
//
// Sets 'Cache-Control: max-age' header if expire > 0.
// Sets the given response status code.
func SendResponseFromFile(rw http.ResponseWriter, f *os.File, expire time.Duration, statusCode int) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("sendRespFromFile: cannot seek to the beginning: %s", err)
	}

	h := rw.Header()

	ct, err := readHeader(f)
	if err != nil {
		return fmt.Errorf("cannot read Content-Type from %q: %s", f.Name(), err)
	}
	if len(ct) > 0 {
		h.Set("Content-Type", ct)
	}
	ce, err := readHeader(f)
	if err != nil {
		return fmt.Errorf("cannot read Content-Encoding from %q: %s", f.Name(), err)
	}
	if len(ce) > 0 {
		h.Set("Content-Encoding", ce)
	}

	// Determine Content-Length
	off, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return fmt.Errorf("cannot determine the current position in %q: %s", f.Name(), err)
	}
	fi, err := f.Stat()
	if err != nil {
		return fmt.Errorf("cannot stat %q: %s", f.Name(), err)
	}
	fs := fi.Size()
	cl := fs - off
	h.Set("Content-Length", fmt.Sprintf("%d", cl))

	setCacheControl(h, expire, fi.ModTime())

	rw.WriteHeader(statusCode)
	if _, err := io.Copy(rw, f); err != nil {
		return fmt.Errorf("cannot send %q to client: %s", f.Name(), err)
	}
	return nil
}

func setCacheControl(h http.Header, expire time.Duration, modifTime time.Time) {
	// Set 'Cache-Control: max-age' on non-temporary file
	if expire > 0 {
		mt := modifTime
		age := time.Since(mt)
		left := expire - age
		if left > 0 {
			leftSeconds := uint(left / time.Second)
			h.Set("Cache-Control", fmt.Sprintf("max-age=%d", leftSeconds))
		}
	}
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
