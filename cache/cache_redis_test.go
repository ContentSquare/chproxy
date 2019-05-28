package cache

import (
	"bytes"
	"fmt"
	"github.com/alicebob/miniredis"
	"github.com/contentsquare/chproxy/config"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRedisCacheClose(t *testing.T) {
	for i := 0; i < 2; i++ {
		c := newRedisTestCache(t)
		c.Close()
	}
}

//func TestRedisCacheAddGet(t *testing.T) {
//	c := newRedisTestCache(t)
//	defer c.Close()
//	var i = 0
//	//for i := 0; i < 10; i++ {
//		key := &Key{
//			Query: []byte(fmt.Sprintf("SELECT %d", i)),
//		}
//		trw := &testRedisResponseWriter{}
//		crw, err := c.NewResponseWriter(trw, key)
//		fmt.Println(fmt.Sprintf("crw is %v", crw))
//
//		if err != nil {
//			t.Fatalf("cannot create response writer: %s", err)
//		}
//
//		ct := fmt.Sprintf("text/html; %d", i)
//		crw.Header().Set("Content-Type", ct)
//		ce := fmt.Sprintf("gzip; %d", i)
//		crw.Header().Set("Content-Encoding", ce)
//
//		value := fmt.Sprintf("value %d", i)
//		bs := bytes.NewBufferString(value)
//		io.Copy(crw, bs)
//		//if _, err := io.Copy(crw, bs); err != nil {
//		//	t.Fatalf("cannot send response to cache: %s", err)
//		//}
//	//	if err := crw.Commit(); err != nil {
//	//		t.Fatalf("cannot commit response to cache: %s", err)
//	//	}
//	//
//	//	// Verify trw contains valid headers.
//	//	gotCT := trw.Header().Get("Content-Type")
//	//	if gotCT != ct {
//	//		t.Fatalf("unexpected Content-Type: %q; expecting %q", gotCT, ct)
//	//	}
//	//	gotCE := trw.Header().Get("Content-Encoding")
//	//	if gotCE != ce {
//	//		t.Fatalf("unexpected Content-Encoding: %q; expecting %q", gotCE, ce)
//	//	}
//	//	cl := fmt.Sprintf("%d", len(value))
//	//	gotCL := trw.Header().Get("Content-Length")
//	//	if gotCL != cl {
//	//		t.Fatalf("unexpected Content-Length: %q; expecting %q", gotCL, cl)
//	//	}
//	//
//	//	// Verify trw contains the response.
//	//	if string(trw.b) != value {
//	//		t.Fatalf("unexpected response sent to client: %q; expecting %q", trw.b, value)
//	//	}
//	//}
//
//	//// Verify the responses are actually cached.
//	//for i := 0; i < 10; i++ {
//	//	key := &Key{
//	//		Query: []byte(fmt.Sprintf("SELECT %d", i)),
//	//	}
//	//	trw := &testRedisResponseWriter{}
//	//	if err := c.WriteTo(trw, key); err != nil {
//	//		t.Fatalf("unexpected error: %s", err)
//	//	}
//	//	value := fmt.Sprintf("value %d", i)
//	//
//	//	ct := fmt.Sprintf("text/html; %d", i)
//	//	gotCT := trw.Header().Get("Content-Type")
//	//	if gotCT != ct {
//	//		t.Fatalf("unexpected Content-Type: %q; expecting %q", gotCT, ct)
//	//	}
//	//	ce := fmt.Sprintf("gzip; %d", i)
//	//	gotCE := trw.Header().Get("Content-Encoding")
//	//	if gotCE != ce {
//	//		t.Fatalf("unexpected Content-Encoding: %q; expecting %q", gotCE, ce)
//	//	}
//	//	cl := fmt.Sprintf("%d", len(value))
//	//	gotCL := trw.Header().Get("Content-Length")
//	//	if gotCL != cl {
//	//		t.Fatalf("unexpected Content-Length: %q; expecting %q", gotCL, cl)
//	//	}
//	//
//	//	if string(trw.b) != value {
//	//		t.Fatalf("unexpected response sent to client: %q; expecting %q", trw.b, value)
//	//	}
//	//}
//
//	// Verify the cache may be re-opened.
//	c1 := newRedisTestCache(t)
//	defer c1.Close()
//
//	//for i := 0; i < 10; i++ {
//	//	key := &Key{
//	//		Query: []byte(fmt.Sprintf("SELECT %d", i)),
//	//	}
//	//	trw := &testRedisResponseWriter{}
//	//	if err := c1.WriteTo(trw, key); err != nil {
//	//		t.Fatalf("unexpected error: %s", err)
//	//	}
//	//	value := fmt.Sprintf("value %d", i)
//	//
//	//	ct := fmt.Sprintf("text/html; %d", i)
//	//	gotCT := trw.Header().Get("Content-Type")
//	//	if gotCT != ct {
//	//		t.Fatalf("unexpected Content-Type: %q; expecting %q", gotCT, ct)
//	//	}
//	//	ce := fmt.Sprintf("gzip; %d", i)
//	//	gotCE := trw.Header().Get("Content-Encoding")
//	//	if gotCE != ce {
//	//		t.Fatalf("unexpected Content-Encoding: %q; expecting %q", gotCE, ce)
//	//	}
//	//	cl := fmt.Sprintf("%d", len(value))
//	//	gotCL := trw.Header().Get("Content-Length")
//	//	if gotCL != cl {
//	//		t.Fatalf("unexpected Content-Length: %q; expecting %q", gotCL, cl)
//	//	}
//	//
//	//	if string(trw.b) != value {
//	//		t.Fatalf("unexpected response sent to client: %q; expecting %q", trw.b, value)
//	//	}
//	//}
//}

func TestRedisCacheAddGet(t *testing.T) {
	c := newRedisTestCache(t)
	defer c.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d", i)),
		}
		trw := &testRedisResponseWriter{}
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
		trw := &testFileResponseWriter{}
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
	c1 := newFileTestCache(t)
	defer c1.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d", i)),
		}
		trw := &testFileResponseWriter{}
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

func TestRedisCacheMiss(t *testing.T) {
	c := newRedisTestCache(t)
	defer c.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d cache miss", i)),
		}
		trw := &testRedisResponseWriter{}
		err := c.WriteTo(trw, key)
		if err == nil {
			t.Fatalf("expecting error")
		}
		if err != ErrMissing {
			t.Fatalf("unexpected error: %s; expecting %s", err, ErrMissing)
		}
	}
}

func TestRedisPendingEntries(t *testing.T) {
	cfg := config.Cache{
		Name:      "foobar",
		RedisHost: "localhost",
		RedisPort: 32771,
		MaxSize:   1e6,
		Expire:    config.Duration(time.Minute),
	}
	c, err := NewCache(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	key := &Key{
		Query: []byte("SELECT pending entries"),
	}
	value := "value for pending entries"

	trw := &testRedisResponseWriter{}
	err = c.WriteTo(trw, key)
	if err == nil {
		t.Fatalf("expecting error")
	}
	if err != ErrMissing {
		t.Fatalf("unexpected error: %s; expecting %s", err, ErrMissing)
	}

	ch := make(chan error)
	go func() {
		trw := &testRedisResponseWriter{}
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
	trw = &testRedisResponseWriter{}
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

func TestRedisCacheRollback(t *testing.T) {
	c := newRedisTestCache(t)
	defer c.Close()

	for i := 0; i < 10; i++ {
		key := &Key{
			Query: []byte(fmt.Sprintf("SELECT %d cache rollback", i)),
		}
		trw := &testRedisResponseWriter{}
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
		trw := &testRedisResponseWriter{}
		err := c.WriteTo(trw, key)
		if err == nil {
			t.Fatalf("expecting non-nil error")
		}
		if err != ErrMissing {
			t.Fatalf("unexpected error: %q; expecting %q", err, ErrMissing)
		}
	}
}

type testRedisResponseWriter struct {
	h http.Header
	b []byte
}

func (trw *testRedisResponseWriter) Write(p []byte) (int, error) {
	trw.b = append(trw.b, p...)
	return len(p), nil
}

func (trw *testRedisResponseWriter) Header() http.Header {
	if trw.h == nil {
		trw.h = make(http.Header)
	}
	return trw.h
}

func (trw *testRedisResponseWriter) WriteHeader(statusCode int) {}

func newRedisTestCache(t *testing.T) Cache {

	t.Helper()

	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	port, _ := strconv.Atoi(s.Port())
	cfg := config.Cache{
		Name:      "foobar",
		RedisHost: s.Host(),
		RedisPort: port,
		MaxSize:   1e6,
		Expire:    config.Duration(time.Minute),
	}
	c, err := NewCache(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
