package cache

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/contentsquare/chproxy/config"
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

func TestFilesystemCacheAddGet(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()
	c1 := newTestCache(t)
	defer c1.Close()
	cacheAddGetHelper(t, c, c1)
}

// metatest used for both filesystem and redis Cache
func cacheAddGetHelper(t *testing.T, c Cache, c1 Cache) {

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d", i)),
		}
		trw := &testResponseWriter{}
		crw := NewBufferedResponseWriter(trw)

		ct := fmt.Sprintf("text/html; %d", i)
		crw.Header().Set("Content-Type", ct)
		ce := fmt.Sprintf("gzip; %d", i)
		crw.Header().Set("Content-Encoding", ce)

		value := fmt.Sprintf("value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}

		buffer := crw.Reader()

		length := int64(len(value))
		if _, err := c.Put(buffer, ContentMetadata{Encoding: ce, Type: ct, Length: length}, key); err != nil {
			t.Fatalf("failed to put it to cache: %s", err)
		}

		cachedData, err := c.Get(key)
		if err != nil {
			t.Fatalf("failed to get data from filesystem cache: %s", err)
		}

		// Verify trw contains valid headers.
		if cachedData.Type != ct {
			t.Fatalf("unexpected Content-Type: %q; expecting %q", cachedData.Type, ct)
		}
		if cachedData.Encoding != ce {
			t.Fatalf("unexpected Content-Encoding: %q; expecting %q", cachedData.Encoding, ce)
		}
		cl := length
		if cachedData.Length != cl {
			t.Fatalf("unexpected Content-Length: %q; expecting %q", cachedData.Length, cl)
		}
		buf := new(strings.Builder)
		_, err = io.Copy(buf, cachedData.Data)
		if err != nil {
			t.Fatalf("couldn't read buffer to string %s", err)
		}
		// Verify trw contains the response.
		if buf.String() != value {
			t.Fatalf("unexpected response sent to client: %q; expecting %q", trw.b, value)
		}
	}

	// Verify the cache may be re-opened.
	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d", i)),
		}
		cachedData, err := c.Get(key)
		if err != nil {
			t.Fatalf("failed to get data from filesystem cache: %s", err)
		}
		value := fmt.Sprintf("value %d", i)
		ct := fmt.Sprintf("text/html; %d", i)
		ce := fmt.Sprintf("gzip; %d", i)
		// Verify trw contains valid headers.
		if cachedData.Type != ct {
			t.Fatalf("unexpected Content-Type: %q; expecting %q", cachedData.Type, ct)
		}
		if cachedData.Encoding != ce {
			t.Fatalf("unexpected Content-Encoding: %q; expecting %q", cachedData.Encoding, ce)
		}
		cl := int64(len(value))
		if cachedData.Length != cl {
			t.Fatalf("unexpected Content-Length: %q; expecting %q", cachedData.Length, cl)
		}
		buf := new(strings.Builder)
		_, err = io.Copy(buf, cachedData.Data)
		if err != nil {
			t.Fatalf("couldn't read buffer to string %s", err)
		}
		// Verify that payloads match.
		if buf.String() != value {
			t.Fatalf("unexpected value found in cache: %q; expecting %q", buf.String(), value)
		}
	}
}

func TestFilesystemCacheMiss(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()
	cacheMissHelper(t, c)
}

// metatest used for both filesystem and redis Cache
func cacheMissHelper(t *testing.T, c Cache) {
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
		crw := NewBufferedResponseWriter(trw)

		value := fmt.Sprintf("very big value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}

		reader := crw.Reader()
		if _, err := c.Put(reader, ContentMetadata{}, key); err != nil {
			t.Fatalf("failed to put it to cache: %s", err)
		}
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
