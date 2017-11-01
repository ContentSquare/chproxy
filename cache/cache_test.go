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

func TestKeyString(t *testing.T) {
	testCases := []struct {
		key      *Key
		expected string
	}{
		{
			key: &Key{
				Query: []byte("SELECT 1 FROM system.numbers LIMIT 10"),
			},
			expected: "c1366f1b0a3e284006c0a1be6e3f1f68",
		},
		{
			key: &Key{
				Query:  []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				IsGzip: true,
			},
			expected: "5544a41f4cedc3b4fff2695a28b00594",
		},
		{
			key: &Key{
				Query:         []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				IsGzip:        true,
				DefaultFormat: "JSON",
			},
			expected: "82fe18538028dfb0317037279ada6377",
		},
		{
			key: &Key{
				Query:         []byte("SELECT 1 FROM system.numbers LIMIT 10"),
				IsGzip:        true,
				DefaultFormat: "JSON",
				Database:      "foobar",
			},
			expected: "3da029b8d18ee11a66f397e38d78b7f5",
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

		value := fmt.Sprintf("value %d", i)
		bs := bytes.NewBufferString(value)
		if _, err := io.Copy(crw, bs); err != nil {
			t.Fatalf("cannot send response to cache: %s", err)
		}
		if err := crw.Commit(); err != nil {
			t.Fatalf("cannot commit response to cache: %s", err)
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
		Expire:    time.Minute,
		GraceTime: 30 * time.Second,
	}
	c, err := newCache(cfg)
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
		Expire:  time.Minute,
	}
	c, err := newCache(cfg)
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
	b []byte
}

func (trw *testResponseWriter) Write(p []byte) (int, error) {
	trw.b = append(trw.b, p...)
	return len(p), nil
}

func (trw *testResponseWriter) Header() http.Header {
	return http.Header{}
}

func (trw *testResponseWriter) WriteHeader(statusCode int) {}

func newTestCache(t *testing.T) *Cache {
	t.Helper()

	cfg := config.Cache{
		Name:    "foobar",
		Dir:     testDir,
		MaxSize: 1e6,
		Expire:  time.Minute,
	}
	c, err := newCache(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
