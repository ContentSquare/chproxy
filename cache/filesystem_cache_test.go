package cache

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/Vertamedia/chproxy/config"
)

const testDir = "./test-data"

func TestMain(m *testing.M) {
	retCode := m.Run()
	if err := os.RemoveAll(testDir); err != nil {
		log.Fatalf("cannot remove %q: %s", testDir, err)
	}
	os.Exit(retCode)
}

func TestWriteReadHeader(t *testing.T) {
	expectedS := "foo-bar1; baz"
	bb := &bytes.Buffer{}
	if err := writeHeader(bb, expectedS); err != nil {
		t.Fatalf("cannot write header: %q", err)
	}

	s, err := readHeader(bb)
	if err != nil {
		t.Fatalf("cannot read header: %q", err)
	}
	if s != expectedS {
		t.Fatalf("unexpected header %q; expecting %q", s, expectedS)
	}
}

func TestCacheClose(t *testing.T) {
	for i := 0; i < 10; i++ {
		c := newTestCache(t)
		c.Close()
	}
}

func TestCacheAddGet(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d", i)),
		}
		trw := &testResponseWriter{}
		crw, err := NewTmpFileResponseWriter(trw, "/tmp")
		if err != nil {
			t.Fatalf("cannot create response writer: %s", err)
		}

		ct := fmt.Sprintf("text/html; %d", i)
		crw.Header().Set("Content-Type", ct)
		ce := fmt.Sprintf("gzip; %d", i)
		crw.Header().Set("Content-Encoding", ce)

		value := fmt.Sprintf("value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}

		f, err := crw.GetFile()
		if err != nil {
			t.Fatalf("cannot get file: %s", err)
		}

		if _, err := c.Put(f, key); err != nil {
			t.Fatalf("failed to put it to cache: %s", err)
		}

		if err := SendResponseFromReader(trw, f, 0*time.Second, http.StatusOK); err != nil {
			t.Fatalf("cannot commit response to cache: %s", err)
		}

		// Verify trw contains valid headers.
		gotCT := trw.Header().Get("Content-Type")
		if gotCT != ct {
			t.Fatalf("unexpected Content-Type: %q; expecting %q", gotCT, ct)
		}
		gotCE := trw.Header().Get("Content-Encoding")
		if gotCE != ce {
			t.Fatalf("unexpected Content-Encoding: %q; expecting %q", gotCE, ce)
		}
		cl := fmt.Sprintf("%d", len(value))
		gotCL := trw.Header().Get("Content-Length")
		if gotCL != cl {
			t.Fatalf("unexpected Content-Length: %q; expecting %q", gotCL, cl)
		}

		// Verify trw contains the response.
		if string(trw.b) != value {
			t.Fatalf("unexpected response sent to client: %q; expecting %q", trw.b, value)
		}
	}

	// Verify the responses are actually cached.
	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d", i)),
		}
		trw := &testResponseWriter{}
		v, err := c.Get(key)

		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if err := SendResponseFromReader(trw, v.Data, v.Ttl, 200); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		value := fmt.Sprintf("value %d", i)

		ct := fmt.Sprintf("text/html; %d", i)
		gotCT := trw.Header().Get("Content-Type")
		if gotCT != ct {
			t.Fatalf("unexpected Content-Type: %q; expecting %q", gotCT, ct)
		}
		ce := fmt.Sprintf("gzip; %d", i)
		gotCE := trw.Header().Get("Content-Encoding")
		if gotCE != ce {
			t.Fatalf("unexpected Content-Encoding: %q; expecting %q", gotCE, ce)
		}
		cl := fmt.Sprintf("%d", len(value))
		gotCL := trw.Header().Get("Content-Length")
		if gotCL != cl {
			t.Fatalf("unexpected Content-Length: %q; expecting %q", gotCL, cl)
		}

		if string(trw.b) != value {
			t.Fatalf("unexpected response sent to client: %q; expecting %q", trw.b, value)
		}
	}

	// Verify the cache may be re-opened.
	c1 := newTestCache(t)
	defer c1.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d", i)),
		}
		trw := &testResponseWriter{}

		v, err := c1.Get(key)

		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		if err := SendResponseFromReader(trw, v.Data, v.Ttl, 200); err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		value := fmt.Sprintf("value %d", i)

		ct := fmt.Sprintf("text/html; %d", i)
		gotCT := trw.Header().Get("Content-Type")
		if gotCT != ct {
			t.Fatalf("unexpected Content-Type: %q; expecting %q", gotCT, ct)
		}
		ce := fmt.Sprintf("gzip; %d", i)
		gotCE := trw.Header().Get("Content-Encoding")
		if gotCE != ce {
			t.Fatalf("unexpected Content-Encoding: %q; expecting %q", gotCE, ce)
		}
		cl := fmt.Sprintf("%d", len(value))
		gotCL := trw.Header().Get("Content-Length")
		if gotCL != cl {
			t.Fatalf("unexpected Content-Length: %q; expecting %q", gotCL, cl)
		}

		if string(trw.b) != value {
			t.Fatalf("unexpected response sent to client: %q; expecting %q", trw.b, value)
		}
	}
}

func TestCacheMiss(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d cache miss", i)),
		}

		_, err := c.Get(key)

		if err != ErrMissing {
			t.Fatalf("unexpected error: %s; expecting %s", err, ErrMissing)
		}
	}
}

func TestCacheRollback(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()

	for i := 0; i < 10; i++ {
		trw := &testResponseWriter{}
		crw, err := NewTmpFileResponseWriter(trw, "/tmp")
		if err != nil {
			t.Fatalf("cannot create response writer: %s", err)
		}

		value := fmt.Sprintf("very big value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}
		if f, err := crw.GetFile(); err == nil {
			if err := SendResponseFromReader(trw, f, 0*time.Second, http.StatusOK); err != nil {
				t.Fatalf("cannot commit response to cache: %s", err)
			}
		} else {
			t.Fatalf("cannot rollback response: %s", err)
		}

		// Verify trw contains valid response
		if string(trw.b) != value {
			t.Fatalf("unexpected value received: %q; expecting %q", trw.b, value)
		}
	}

	// Verify that rolled back values aren't cached
	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d cache rollback", i)),
		}
		_, err := c.Get(key)
		if err == nil {
			t.Fatalf("expecting non-nil error")
		}
		if err != ErrMissing {
			t.Fatalf("unexpected error: %q; expecting %q", err, ErrMissing)
		}
	}
}

func TestCacheClean(t *testing.T) {
	cfg := config.Cache{
		Name: "foobar",
		FileSystem: config.FileSystemCacheConfig{
			Dir:     testDir,
			MaxSize: 8192,
		},
		Expire: config.Duration(time.Minute),
	}
	c, err := newFilesSystemCache(cfg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// populate the cache with a lot of entries
	for i := 0; i < 1000; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d cache clean", i)),
		}
		trw := &testResponseWriter{}
		crw, err := NewTmpFileResponseWriter(trw, "/tmp")
		if err != nil {
			t.Fatalf("cannot create response writer: %s", err)
		}

		value := fmt.Sprintf("very big value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}

		f, err := crw.GetFile()
		if err != nil {
			t.Fatalf("cannot get file: %s", err)
		}

		if _, err := c.Put(f, key); err != nil {
			t.Fatalf("failed to put it to cache: %s", err)
		}
		crw.Close()
	}

	// Forcibly clean the cache
	c.clean()

	// Make sure the total cache size doesnt exceed MaxSize
	stats := c.Stats()
	if stats.Size <= 0 {
		t.Fatalf("cache size must be greater than 0; got %d", stats.Size)
	}
	if stats.Size > c.maxSize {
		t.Fatalf("cache size %d cannot exceed %d", stats.Size, c.maxSize)
	}

	if stats.Items <= 0 {
		t.Fatalf("cache items must be greater than 0; got %d", stats.Items)
	}
	if stats.Items > 1000 {
		t.Fatalf("cache items %d cannot exceed %d", stats.Items, 1000)
	}
}

type testResponseWriter struct {
	h http.Header
	b []byte
}

func (trw *testResponseWriter) Write(p []byte) (int, error) {
	trw.b = append(trw.b, p...)
	return len(p), nil
}

func (trw *testResponseWriter) Header() http.Header {
	if trw.h == nil {
		trw.h = make(http.Header)
	}
	return trw.h
}

func (trw *testResponseWriter) WriteHeader(statusCode int) {}

func newTestCache(t *testing.T) *fileSystemCache {
	t.Helper()

	cfg := config.Cache{
		Name: "foobar",
		FileSystem: config.FileSystemCacheConfig{
			Dir:     testDir,
			MaxSize: 1e6,
		},
		Expire: config.Duration(time.Minute),
	}
	c, err := newFilesSystemCache(cfg, 1*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
