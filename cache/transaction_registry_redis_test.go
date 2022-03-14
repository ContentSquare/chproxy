package cache

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func TestRedisTransaction(t *testing.T) {
	s, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	defer s.Close()

	redisClient := redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{s.Addr()},
	})

	graceTime := 10 * time.Second
	key := &Key{
		Query: []byte("SELECT pending entries"),
	}

	redisTransaction := newRedisTransactionRegistry(redisClient, graceTime)

	if err := redisTransaction.Register(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	isDone := redisTransaction.IsDone(key)
	if isDone {
		t.Fatalf("unexpected: transaction should be pending")
	}

	if err := redisTransaction.Unregister(key); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	isDone = redisTransaction.IsDone(key)
	if !isDone {
		t.Fatalf("unexpected: transaction should be done")
	}

}
