package cache

import (
	"github.com/Vertamedia/chproxy/config"
	"os"
	"testing"
	"time"
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
	done := asyncCache.AwaitForConcurrentTransaction(key)
	elapsedTime := time.Since(startTime)

	// in order to let the cleaner swipe the transaction
	time.Sleep(100 * time.Millisecond)
	if done == asyncCache.IsDone(key) && done {
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
	done := asyncCache.AwaitForConcurrentTransaction(key)
	elapsedTime := time.Since(startTime)

	err := <-errs
	if err != nil {
		t.Fatalf("unexpected error: %s failed to unregister transaction", err)
	}

	if done != asyncCache.IsDone(key) || !done || elapsedTime >= graceTime {
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
