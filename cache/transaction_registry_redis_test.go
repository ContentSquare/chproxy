package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
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

	redisTransaction := newRedisTransactionRegistry(redisClient, graceTime)

	if err := redisTransaction.Create(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	status, err := redisTransaction.Status(key)
	if err != nil || !status.IsPending() {
		t.Fatalf("unexpected: transaction should be pending")
	}

	if err := redisTransaction.Complete(key); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	status, err = redisTransaction.Status(key)
	if err != nil || !status.IsCompleted() {
		t.Fatalf("unexpected: transaction should be done")
	}
}
