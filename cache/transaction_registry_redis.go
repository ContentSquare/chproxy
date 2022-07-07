package cache

import (
	"context"
	"errors"
	"time"

	"fmt"

	"github.com/contentsquare/chproxy/log"
	"github.com/go-redis/redis/v8"
)

type redisTransactionRegistry struct {
	redisClient redis.UniversalClient
	// deadline specifies TTL of record to be kept, no matter the associated transaction status
	deadline time.Duration
}

func newRedisTransactionRegistry(redisClient redis.UniversalClient, deadline time.Duration) *redisTransactionRegistry {
	return &redisTransactionRegistry{
		redisClient: redisClient,
		deadline:    deadline,
	}
}

func (r *redisTransactionRegistry) Create(key *Key) error {
	return r.redisClient.Set(context.Background(), toTransactionKey(key), uint64(transactionCreated), r.deadline).Err()
}

func (r *redisTransactionRegistry) Complete(key *Key) error {
	return r.updateTransactionState(key, transactionCompleted)
}

func (r *redisTransactionRegistry) Fail(key *Key) error {
	return r.updateTransactionState(key, transactionFailed)
}

func (r *redisTransactionRegistry) updateTransactionState(key *Key, state TransactionState) error {
	return r.redisClient.Set(context.Background(), toTransactionKey(key), uint64(state), r.deadline).Err()
}

func (r *redisTransactionRegistry) Status(key *Key) (TransactionState, error) {
	state, err := r.redisClient.Get(context.Background(), toTransactionKey(key)).Uint64()
	if errors.Is(err, redis.Nil) {
		return transactionAbsent, nil
	}

	if err != nil {
		log.Errorf("Failed to fetch transaction status from redis for key: %s", key.String())
		return transactionAbsent, err
	}

	return TransactionState(state), nil
}

func (r *redisTransactionRegistry) Close() error {
	return r.redisClient.Close()
}

func toTransactionKey(key *Key) string {
	return fmt.Sprintf("%s-transaction", key.String())
}
