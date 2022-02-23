package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
)

type MockUniversalClient struct {
	dict map[string]string
}

func (c *MockUniversalClient) Close() error {
	return nil
}

// the following SET/DEL/GET implement a very simple in memory K/V
func (c *MockUniversalClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	c.dict[key] = fmt.Sprint(value)
	return redis.NewStatusCmd(ctx)
}
func (c *MockUniversalClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	for _, key := range keys {
		delete(c.dict, key)
	}
	return redis.NewIntCmd(ctx)
}
func (c *MockUniversalClient) Get(ctx context.Context, key string) *redis.StringCmd {
	stringCmd := redis.NewStringCmd(ctx)
	val, prs := c.dict[key]
	if prs {
		stringCmd.SetVal(val)
		return stringCmd
	} else {
		stringCmd.SetErr(redis.Nil)
	}
	return stringCmd
}

func TestRedisTransaction(t *testing.T) {

	graceTime := 10 * time.Second
	key := &Key{
		Query: []byte("SELECT pending entries"),
	}
	mockRedis := MockUniversalClient{dict: make(map[string]string)}
	redisTransaction := newRedisTransactionRegistry(&mockRedis, graceTime)

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
