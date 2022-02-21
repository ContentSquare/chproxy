package cache

import (
	"log"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/contentsquare/chproxy/config"
)

const asyncTestDir = "./async-test-data"

func TestAsyncCache_Cleanup_Of_Expired_Transactions(t *testing.T) {
	graceTime := 100 * time.Millisecond
	asyncCache := newAsyncTestCache(t, graceTime)

	key := &Key{
		Query: []byte("SELECT async cache"),
	}

	if done := asyncCache.IsDone(key); !done {
		t.Fatalf("unexpected behaviour: transaction isnt done while it wasnt even started")
	}

	if err := asyncCache.Register(key); err != nil {
		t.Fatalf("unexpected error: %s failed to register transaction", err)
	}

	if done := asyncCache.IsDone(key); done {
		t.Fatalf("unexpected behaviour: transaction isnt finished")
	}

	time.Sleep(graceTime * 2)

	if done := asyncCache.IsDone(key); !done {
		t.Fatalf("unexpected behaviour: transaction grace time elapsed and yet it was still pending")
	}

	asyncCache.Close()
	os.RemoveAll(asyncTestDir)
}

func TestAsyncCache_AwaitForConcurrentTransaction_GraceTimeWithoutTransactionCompletion(t *testing.T) {
	graceTime := 300 * time.Millisecond
	asyncCache := newAsyncTestCache(t, graceTime)

	key := &Key{
		Query: []byte("SELECT async cache AwaitForConcurrentTransaction"),
	}

	if done := asyncCache.IsDone(key); !done {
		t.Fatalf("unexpected behaviour: transaction isnt done while it wasnt even started")
	}

	if err := asyncCache.Register(key); err != nil {
		t.Fatalf("unexpected error: %s failed to register transaction", err)
	}

	if done := asyncCache.IsDone(key); done {
		t.Fatalf("unexpected behaviour: transaction isnt finished")
	}

	startTime := time.Now()
	transactionResult := asyncCache.AwaitForConcurrentTransaction(key)
	elapsedTime := time.Since(startTime)

	// in order to let the cleaner swipe the transaction
	time.Sleep(100 * time.Millisecond)
	if transactionResult.Completed == asyncCache.IsDone(key) && transactionResult.Completed {
		t.Fatalf("unexpected behaviour: transaction awaiting time elapsed %s", elapsedTime.String())
	}

	asyncCache.Close()
	os.RemoveAll(asyncTestDir)
}

func TestAsyncCache_AwaitForConcurrentTransaction_TransactionCompletedWhileAwaiting(t *testing.T) {
	graceTime := 300 * time.Millisecond
	asyncCache := newAsyncTestCache(t, graceTime)

	key := &Key{
		Query: []byte("SELECT async cache AwaitForConcurrentTransactionCompleted"),
	}

	if err := asyncCache.Register(key); err != nil {
		t.Fatalf("unexpected error: %s failed to register transaction", err)
	}

	errs := make(chan error)
	go func() {
		time.Sleep(graceTime / 2)
		if err := asyncCache.Unregister(key); err != nil {
			errs <- err
		} else {
			errs <- nil
		}
	}()

	startTime := time.Now()
	transactionResult := asyncCache.AwaitForConcurrentTransaction(key)
	elapsedTime := time.Since(startTime)

	err := <-errs
	if err != nil {
		t.Fatalf("unexpected error: %s failed to unregister transaction", err)
	}

	if transactionResult.Completed != asyncCache.IsDone(key) || !transactionResult.Completed || elapsedTime >= graceTime {
		t.Fatalf("unexpected behaviour: transaction awaiting time elapsed %s", elapsedTime.String())
	}

	asyncCache.Close()
	os.RemoveAll(asyncTestDir)
}

func newAsyncTestCache(t *testing.T, graceTime time.Duration) *AsyncCache {
	t.Helper()
	cfg := config.Cache{
		Name: "foobar",
		FileSystem: config.FileSystemCacheConfig{
			Dir:     asyncTestDir,
			MaxSize: 1e6,
		},
		Expire: config.Duration(time.Minute),
	}
	c, err := newFilesSystemCache(cfg, graceTime)
	if err != nil {
		t.Fatal(err)
	}

	asyncC := &AsyncCache{
		Cache:               c,
		TransactionRegistry: newInMemoryTransactionRegistry(graceTime),
		graceTime:           graceTime,
	}
	return asyncC
}

func TestAsyncCache_FilesystemCache_instantiation(t *testing.T) {
	const testDirAsync = "./test-data-async"
	fileSystemCfg := config.Cache{
		Name: "test",
		Mode: "file_system",
		FileSystem: config.FileSystemCacheConfig{
			Dir:     asyncTestDir,
			MaxSize: 8192,
		},
		Expire: config.Duration(time.Minute),
	}
	if err := os.RemoveAll(testDirAsync); err != nil {
		log.Fatalf("cannot remove %q: %s", testDirAsync, err)
	}
	_, err := NewAsyncCache(fileSystemCfg)
	if err != nil {
		t.Fatalf("could not instanciate filsystem async cache because of the following error: %s", err)
	}
}

func TestAsyncCache_FilesystemCache_wrong_instantiation(t *testing.T) {
	fileSystemCfg := config.Cache{
		Name: "test",
		Mode: "file_system",
		FileSystem: config.FileSystemCacheConfig{
			Dir:     "",
			MaxSize: 8192,
		},
		Expire: config.Duration(time.Minute),
	}
	_, err := NewAsyncCache(fileSystemCfg)
	if err == nil {
		t.Fatalf("the instanciate of filsystem async cache should have crashed")
	}
}

func TestAsyncCache_RedisCache_instantiation(t *testing.T) {
	s := miniredis.RunT(t)
	var redisCfg = config.Cache{
		Name: "test",
		Mode: "redis",
		Redis: config.RedisCacheConfig{
			Addresses: []string{s.Addr()},
		},
		Expire: config.Duration(cacheTTL),
	}

	_, err := NewAsyncCache(redisCfg)
	if err != nil {
		t.Fatalf("could not instanciate redis async cache because of the following error: %s", err)
	}
}

func TestAsyncCache_RedisCache_wrong_instantiation(t *testing.T) {
	var redisCfg = config.Cache{
		Name: "test",
		Mode: "redis",
		Redis: config.RedisCacheConfig{
			// fake address
			Addresses: []string{"127.12.0.10:1024"},
		},
	}

	_, err := NewAsyncCache(redisCfg)
	if err == nil {
		t.Fatalf("the redis instanciation should have crashed")
	}
}

func TestAsyncCache_Unkown_instantiation(t *testing.T) {
	var redisCfg = config.Cache{
		Name:   "test",
		Mode:   "Unkown Mode",
		Redis:  config.RedisCacheConfig{},
		Expire: config.Duration(cacheTTL),
	}

	_, err := NewAsyncCache(redisCfg)
	if err == nil {
		t.Fatalf("The instanciation should have crash")
	}
}
