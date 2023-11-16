package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisTransaction(t *testing.T) {
	s := miniredis.RunT(t)

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{s.Addr()},
	})

	graceTime := 10 * time.Second
	key := &Key{
		Query: []byte("SELECT pending entries"),
	}

	redisTransaction := newRedisTransactionRegistry(redisClient, graceTime, graceTime)

	if err := redisTransaction.Create(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	status, err := redisTransaction.Status(key)
	if err != nil || !status.State.IsPending() {
		t.Fatalf("unexpected: transaction should be pending")
	}

	if err := redisTransaction.Complete(key); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	status, err = redisTransaction.Status(key)
	if err != nil || !status.State.IsCompleted() {
		t.Fatalf("unexpected: transaction should be done")
	}
}

func TestFailRedisTransaction(t *testing.T) {
	s := miniredis.RunT(t)

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{s.Addr()},
	})

	graceTime := 10 * time.Second
	key := &Key{
		Query: []byte("SELECT pending entries"),
	}

	redisTransaction := newRedisTransactionRegistry(redisClient, graceTime, graceTime)

	if err := redisTransaction.Create(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	status, err := redisTransaction.Status(key)
	if err != nil || !status.State.IsPending() {
		t.Fatalf("unexpected: transaction should be pending")
	}

	failReason := "failed for fun dudes"

	if err := redisTransaction.Fail(key, failReason); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	status, err = redisTransaction.Status(key)
	if err != nil || !status.State.IsFailed() {
		t.Fatalf("unexpected: transaction should be failed")
	}

	if status.FailReason != failReason {
		t.Fatalf("unexpected: transaction should curry fail reason")
	}
}

func TestCleanupFailedRedisTransaction(t *testing.T) {
	s := miniredis.RunT(t)

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{s.Addr()},
	})

	graceTime := 10 * time.Second
	updatedTTL := 10 * time.Millisecond
	key := &Key{
		Query: []byte("SELECT pending entries"),
	}

	redisTransaction := newRedisTransactionRegistry(redisClient, graceTime, updatedTTL)

	if err := redisTransaction.Create(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	status, err := redisTransaction.Status(key)
	if err != nil || !status.State.IsPending() {
		t.Fatalf("unexpected: transaction should be pending")
	}

	failReason := "failed for fun dudes"

	if err := redisTransaction.Fail(key, failReason); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	status, err = redisTransaction.Status(key)
	if err != nil || !status.State.IsFailed() {
		t.Fatalf("unexpected: transaction should be failed")
	}

	if status.FailReason != failReason {
		t.Fatalf("unexpected: transaction should curry fail reason")
	}

	// move ttls of mini redis to trigger the clean up transaction
	s.FastForward(updatedTTL)

	status, err = redisTransaction.Status(key)
	if err != nil || !status.State.IsAbsent() {
		t.Fatalf("unexpected: transaction should be cleaned up")
	}
}

func TestCleanupCompletedRedisTransaction(t *testing.T) {
	s := miniredis.RunT(t)

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{s.Addr()},
	})

	graceTime := 10 * time.Second
	updatedTTL := 10 * time.Millisecond
	key := &Key{
		Query: []byte("SELECT pending entries"),
	}

	redisTransaction := newRedisTransactionRegistry(redisClient, graceTime, updatedTTL)

	if err := redisTransaction.Create(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	status, err := redisTransaction.Status(key)
	if err != nil || !status.State.IsPending() {
		t.Fatalf("unexpected: transaction should be pending")
	}

	if err := redisTransaction.Complete(key); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	status, err = redisTransaction.Status(key)
	if err != nil || !status.State.IsCompleted() {
		t.Fatalf("unexpected: transaction should be failed")
	}

	// move ttls of mini redis to trigger the clean up transaction
	s.FastForward(updatedTTL)

	status, err = redisTransaction.Status(key)
	if err != nil || !status.State.IsAbsent() {
		t.Fatalf("unexpected: transaction should be cleaned up")
	}
}
