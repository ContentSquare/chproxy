package cache

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Vertamedia/chproxy/config"
	"github.com/Vertamedia/chproxy/log"
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

func TestController(t *testing.T) {
	dir := testDir + "/TestController_Get"
	cfg := config.Cache{
		Name:    "TestController_Get",
		Dir:     dir,
		MaxSize: config.ByteSize(1024),
		Expire:  time.Millisecond * 100,
	}
	MustRegister(cfg)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("err while getting folder %q info: %s", dir, err)
	}
	cc := GetController(cfg.Name)
	if cc == nil {
		t.Fatalf("nil pointer; expected pointer to %s cache controller", cfg.Name)
	}

	k := "key"
	rw := httptest.NewRecorder()
	err := store(cc, rw, k)
	if err != nil {
		t.Fatalf("unexpected err: %s", err)
	}
	if err := compareResponse(rw, "body"); err != nil {
		t.Fatal(err)
	}

	rw2 := httptest.NewRecorder()
	if _, err = cc.WriteTo(rw2, k); err != nil {
		t.Fatal(err)
	}
	if err := compareResponse(rw2, "body"); err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Millisecond * 120)
	rw3 := httptest.NewRecorder()
	if _, err = cc.WriteTo(rw3, k); err == nil {
		t.Fatal("expected to get empty response")
	}
}

func compareResponse(rr *httptest.ResponseRecorder, expected string) error {
	response, err := ioutil.ReadAll(rr.Body)
	if err != nil {
		return fmt.Errorf("error while reading response: %s", err)
	}
	if string(response) != expected {
		return fmt.Errorf("expected: %q; got: %q", expected, string(response))
	}
	return nil
}

func store(cc *Controller, rw http.ResponseWriter, key string) error {
	cw, err := cc.NewResponseWriter(rw)
	if err != nil {
		return err
	}
	if _, err := os.Stat(cw.file.Name()); err != nil {
		return fmt.Errorf("err while getting file %q info: %s", cw.file.Name(), err)
	}
	cw.Write([]byte("body"))
	if err := cc.Flush(key, cw); err != nil {
		return fmt.Errorf("error while flushing cache file: %s", err)
	}
	_, err = cc.WriteTo(rw, key)
	return err
}

func TestCleanup(t *testing.T) {
	dir := testDir + "/TestCleanup"
	cfg := config.Cache{
		Name:    "TestCleanup",
		Dir:     dir,
		Expire:  time.Millisecond * 100,
		MaxSize: config.ByteSize(100),
	}
	MustRegister(cfg)
	cc := GetController(cfg.Name)
	key1 := "key1"
	rw1 := httptest.NewRecorder()
	err := store(cc, rw1, key1)
	if err != nil {
		t.Fatalf("unexpected err: %s", err)
	}
	time.Sleep(time.Millisecond * 50)
	key2 := "key2"
	rw2 := httptest.NewRecorder()
	err = store(cc, rw2, key2)
	if err != nil {
		t.Fatalf("unexpected err: %s", err)
	}

	rw3 := httptest.NewRecorder()
	if _, err = cc.WriteTo(rw3, key1); err != nil {
		t.Fatalf("expected key %q in cache reigster; got: %s", key1, err)
	}

	if _, err = cc.WriteTo(rw3, key2); err != nil {
		t.Fatalf("expected key %q in cache reigster;  got: %s", key2, err)
	}

	time.Sleep(time.Millisecond * 60)
	cc.cleanup()
	if _, err = cc.WriteTo(rw3, key1); err == nil {
		t.Fatalf("expected key %q to be removed from cache reigster", key1)
	}

	time.Sleep(time.Millisecond * 100)
	cc.cleanup()
	if _, err = cc.WriteTo(rw3, key2); err == nil {
		t.Fatalf("expected key %q to be removed from cache reigster", key2)
	}

	if len(cc.registry) != 0 {
		t.Fatalf("expected zero-length registry; got: %d", len(cc.registry))
	}
}

func TestCleanup2(t *testing.T) {
	dir := testDir + "/TestCleanup2"
	cfg := config.Cache{
		Name:    "TestCleanup2",
		Dir:     dir,
		Expire:  time.Second * 10,
		MaxSize: config.ByteSize(30),
	}
	MustRegister(cfg)
	cc := GetController(cfg.Name)

	// every file for 4 bytes
	// so it would be 40 after 10 iterations
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		rw := httptest.NewRecorder()
		err := store(cc, rw, key)
		if err != nil {
			t.Fatalf("unexpected err: %s", err)
		}
		time.Sleep(time.Millisecond * 5)
	}

	// cache must be exceeded MaxSize for 10 bytes
	cc.cleanup()
	// if every file was 4 bytes than
	// we must have 10/4 = 3 extra files
	// so after cleanup it must be 10 - 3 = 7
	if len(cc.registry) != 7 {
		t.Fatalf("expected length: 7; got: %d", len(cc.registry))
	}

	// or 7*4 = 28 size
	if cc.size != int64(28) {
		t.Fatalf("expected size: 28; got: %d", cc.size)
	}

	rw := httptest.NewRecorder()
	// and all keys must lower than 3 number must be absent in registry
	// since they are the oldest
	for i := 0; i < 3; i++ {
		key := fmt.Sprintf("key-%d", i)
		if _, err := cc.WriteTo(rw, key); err == nil {
			t.Fatalf("expected key %q to be absent in registry", key)
		}
	}

	// and all keys higher than 3 - to be present in registry
	for i := 3; i < 10; i++ {
		key := fmt.Sprintf("key-%d", i)
		if _, err := cc.WriteTo(rw, key); err != nil {
			t.Fatalf("expected key %q to be in registry", key)
		}
	}
}
