package cache

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
	"time"
)

var testDir = "./test-data"

func TestMain(m *testing.M) {
	if _, err := os.Stat(testDir); os.IsNotExist(err) {
		os.Mkdir(testDir, os.ModePerm)
	}

	log.SuppressOutput(true)
	retCode := m.Run()
	log.SuppressOutput(false)

	if err := os.RemoveAll(testDir); err != nil {
		log.Fatalf("cannot remove %q: %s", testDir, err)
	}
	os.Exit(retCode)
}

func TestGenerateKey(t *testing.T) {
	testCases := []struct {
		uri      []byte
		body     []byte
		expected string
	}{
		{
			uri:      []byte("http://localhost:8123/?"),
			body:     []byte("SELECT 1 FORMAT Pretty"),
			expected: "8193f45cb25b311bb0ce6aa3e79237f952ff1054",
		},
		{
			uri:      []byte("http://localhost:8123/?query=SELECT%201%20FORMAT%20Pretty"),
			body:     []byte(""),
			expected: "5c6a5430b3364921e941bc07165ae1d69e6bbc50",
		},
	}

	for _, tc := range testCases {
		key := GenerateKey(tc.uri, tc.body)
		if key != tc.expected {
			t.Fatalf("unexpected key value: %s; expected: %s", key, tc.expected)
		}
	}
}

func TestMustRegister(t *testing.T) {
}

func TestController_Store(t *testing.T) {
	dir := testDir + "/TestController_Store"
	cfg := &config.Cache{
		Name:    "TestController_Store",
		Dir:     dir,
		MaxSize: config.ByteSize(1024),
	}
	MustRegister(cfg)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("err while getting folder %q info: %s", dir, err)
	}
	cc := CacheController(cfg.Name)
	if cc == nil {
		t.Fatalf("nil pointer; expected pointer to %s cache controller", cfg.Name)
	}

	body := bytes.NewBufferString("SELECT 1")
	req := httptest.NewRequest("POST", "http://localhost:8123", body)
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("Error reading body: %v", err)
	}
	resp1 := responseWithBody("1")
	resp2 := responseWithBody("1")

	var buf bytes.Buffer
	resp2.Write(&buf)

	// generate key and store to cache
	key := GenerateKey(UnsafeStr2Bytes(req.RequestURI), b)
	cc.Store(key, resp1)

	// check that cached file exists
	cacheFilePath := fmt.Sprintf("%s/%s", dir, key)
	if _, err := os.Stat(cacheFilePath); err != nil {
		t.Fatalf("err while getting file %q info: %s", cacheFilePath, err)
	}
	cachedResp, ok := cc.Get(key)
	if !ok {
		t.Fatalf("got nil; expected cached response")
	}

	if string(cachedResp) != buf.String() {
		t.Fatalf("got cached resp: %q; expected: %q", string(cachedResp), buf.String())
	}
}

func TestCleanup(t *testing.T) {
	dir := testDir + "/TestCleanup"
	cfg := &config.Cache{
		Name:    "TestCleanup",
		Dir:     dir,
		Expire:  time.Millisecond * 100,
		MaxSize: config.ByteSize(100),
	}
	MustRegister(cfg)
	cc := CacheController(cfg.Name)
	key1 := "key1"
	cc.Store(key1, responseWithBody("body"))
	time.Sleep(time.Millisecond * 50)
	key2 := "key2"
	cc.Store(key2, responseWithBody("body2"))

	_, ok := cc.Get(key1)
	if !ok {
		t.Fatalf("expected key %q in cache reigster; got nil", key1)
	}
	_, ok = cc.Get(key2)
	if !ok {
		t.Fatalf("expected key %q in cache reigster; got nil", key2)
	}

	time.Sleep(time.Millisecond * 60)
	cc.cleanup()
	_, ok = cc.Get(key1)
	if ok {
		t.Fatalf("expected key %q to be removed from cache reigster", key1)
	}

	time.Sleep(time.Millisecond * 100)
	cc.cleanup()
	_, ok = cc.Get(key2)
	if ok {
		t.Fatalf("expected key %q to be removed from cache reigster", key2)
	}

	if len(cc.registry) != 0 {
		t.Fatalf("expected zero-length registry; got: %d", len(cc.registry))
	}
}

func TestCleanup2(t *testing.T) {
	dir := testDir + "/TestCleanup2"
	cfg := &config.Cache{
		Name:    "TestCleanup2",
		Dir:     dir,
		Expire:  time.Second * 10,
		MaxSize: config.ByteSize(140),
	}
	MustRegister(cfg)
	cc := CacheController(cfg.Name)

	// every file for 34 bytes
	// so it would be 340 bytes after 10 iterations
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		cc.Store(key, responseWithBody("body"))
		time.Sleep(time.Millisecond * 5)
	}

	// cache must be exceeded MaxSize for 200 bytes
	cc.cleanup()
	// if every file was 34 bytes than
	// we must have 200/34 = 6 extra files
	// so after cleanup it must be 10 - 6 = 4
	if len(cc.registry) != 4 {
		t.Fatalf("expected length: 4; got: %d", len(cc.registry))
	}

	// or 4*34 = 136 size
	if cc.size != int64(136) {
		t.Fatalf("expected size: 136; got: %d", cc.size)
	}

	// and all keys must lower than 5 number must be absent in registry
	// since they are the oldest
	for i := 0; i < 6; i++ {
		key := fmt.Sprintf("key-%d", i)
		if _, ok := cc.Get(key); ok {
			t.Fatalf("expected key %q to be absent in registry", key)
		}
	}

	// and all keys higher than 5 - to be present in registry
	for i := 6; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		if _, ok := cc.Get(key); !ok {
			t.Fatalf("expected key %q to be in registry", key)
		}
	}

}

func responseWithBody(b string) *http.Response {
	return &http.Response{
		Body: ioutil.NopCloser(bytes.NewBufferString(b)),
	}
}
