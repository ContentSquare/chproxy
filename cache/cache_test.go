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

func TestKeyString(t *testing.T) {
	testCases := []struct {
		key      *Key
		expected string
	}{
		{
			key: &Key{
				Query: []byte("SELECT 1 FROM system.numbers LIMIT 10"),
			},
			expected: "bebe3382e36ffdeea479b45d827b208a",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
			},
			expected: "498c1af30fb94280fd7c7225c0c8fb39",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
			},
			expected: "720292aa0647cc5e53e0b6e6033eef34",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
			},
			expected: "5c6a70736d71e570faca739c4557780c",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Namespace:      "ns123",
			},
			expected: "08b4baf6825e53bbd18136a88abda4f8",
		},
		{
			key: &Key{
				Query:          []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				AcceptEncoding: "gzip",
				DefaultFormat:  "JSON",
				Database:       "foobar",
				Compress:       "1",
				Namespace:      "ns123",
			},
			expected: "0e043f23ccd1b9039b33623b3b7c114a",
		},
	}

	for _, tc := range testCases {
		s := tc.key.String()
		if !cachefileRegexp.MatchString(s) {
			t.Fatalf("invalid key string format: %q", s)
		}
		if s != tc.expected {
			t.Fatalf("unexpected key string: %q; expecting: %q", s, tc.expected)
		}
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
		crw, err := c.NewResponseWriter(trw, key)
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
		if err := crw.Commit(); err != nil {
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
		if err := c.WriteTo(trw, key); err != nil {
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
		if err := c1.WriteTo(trw, key); err != nil {
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
		trw := &testResponseWriter{}
		err := c.WriteTo(trw, key)
		if err == nil {
			t.Fatalf("expecting error")
		}
		if err != ErrMissing {
			t.Fatalf("unexpected error: %s; expecting %s", err, ErrMissing)
		}
	}
}

func TestPendingEntries(t *testing.T) {
	cfg := config.Cache{
		Name:      "foobar",
		Dir:       testDir,
		MaxSize:   1e6,
		Expire:    config.Duration(time.Minute),
		GraceTime: config.Duration(30 * time.Second),
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	key := &Key{
		Query: []byte("SELECT pending entries"),
	}
	value := "value for pending entries"

	trw := &testResponseWriter{}
	err = c.WriteTo(trw, key)
	if err == nil {
		t.Fatalf("expecting error")
	}
	if err != ErrMissing {
		t.Fatalf("unexpected error: %s; expecting %s", err, ErrMissing)
	}

	ch := make(chan error)
	go func() {
		trw := &testResponseWriter{}
		// This should be delayed until the main goroutine writes
		// the value for the given key.
		if err := c.WriteTo(trw, key); err != nil {
			ch <- fmt.Errorf("error in WriteTo: %s", err)
			return
		}
		if string(trw.b) != value {
			ch <- fmt.Errorf("unexpected response sent to client: %q; expecting %q", trw.b, value)
			return
		}
		ch <- nil
	}()

	// Verify that the started goroutine is blocked.
	select {
	case err := <-ch:
		t.Fatalf("the goroutine prematurely finished and returned err: %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	// Write the value to the cache.
	trw = &testResponseWriter{}
	crw, err := c.NewResponseWriter(trw, key)
	if err != nil {
		t.Fatalf("cannot create response writer: %s", err)
	}

	bs := bytes.NewBufferString(value)
	if _, err := io.Copy(crw, bs); err != nil {
		t.Fatalf("cannot send response to cache: %s", err)
	}
	if err := crw.Commit(); err != nil {
		t.Fatalf("cannot commit response to cache: %s", err)
	}

	// Verify that the started goroutine exits.
	select {
	case <-time.After(3 * time.Second):
	case err := <-ch:
		if err != nil {
			t.Fatalf("unexpected error returned from the goroutine: %s", err)
		}
	}
}

func TestCacheRollback(t *testing.T) {
	c := newTestCache(t)
	defer c.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d cache rollback", i)),
		}
		trw := &testResponseWriter{}
		crw, err := c.NewResponseWriter(trw, key)
		if err != nil {
			t.Fatalf("cannot create response writer: %s", err)
		}

		value := fmt.Sprintf("very big value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}
		if err := crw.Rollback(); err != nil {
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
		trw := &testResponseWriter{}
		err := c.WriteTo(trw, key)
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
		Name:    "foobar",
		Dir:     testDir,
		MaxSize: 8192,
		Expire:  config.Duration(time.Minute),
	}
	c, err := New(cfg)
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
		crw, err := c.NewResponseWriter(trw, key)
		if err != nil {
			t.Fatalf("cannot create response writer: %s", err)
		}

		value := fmt.Sprintf("very big value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}
		if err := crw.Commit(); err != nil {
			t.Fatalf("cannot commit response to cache: %s", err)
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

func newTestCache(t *testing.T) *Cache {
	t.Helper()

	cfg := config.Cache{
		Name:    "foobar",
		Dir:     testDir,
		MaxSize: 1e6,
		Expire:  config.Duration(time.Minute),
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
