package cache

import (
	"context"
	"errors"
	"time"

	"fmt"

	"github.com/contentsquare/chproxy/log"
	"github.com/redis/go-redis/v9"
)

type redisTransactionRegistry struct {
	redisClient redis.UniversalClient

	// deadline specifies TTL of the record to be kept
	deadline time.Duration

	// transactionEndedDeadline specifies TTL of the record to be kept that has been ended (either completed or failed)
	transactionEndedDeadline time.Duration
}

func newRedisTransactionRegistry(redisClient redis.UniversalClient, deadline time.Duration,
	endedDeadline time.Duration) *redisTransactionRegistry {
	return &redisTransactionRegistry{
		redisClient:              redisClient,
		deadline:                 deadline,
		transactionEndedDeadline: endedDeadline,
	}
}

func (r *redisTransactionRegistry) Create(key *Key) error {
	return r.redisClient.Set(context.Background(), toTransactionKey(key),
		[]byte{uint8(transactionCreated)}, r.deadline).Err()
}

func (r *redisTransactionRegistry) Complete(key *Key) error {
	return r.updateTransactionState(key, []byte{uint8(transactionCompleted)})
}

func (r *redisTransactionRegistry) Fail(key *Key, reason string) error {
	b := make([]byte, 0, uint32(len(reason))+1)
	b = append(b, byte(transactionFailed))
	b = append(b, []byte(reason)...)
	return r.updateTransactionState(key, b)
}

func (r *redisTransactionRegistry) updateTransactionState(key *Key, value []byte) error {
	return r.redisClient.Set(context.Background(), toTransactionKey(key), value, r.transactionEndedDeadline).Err()
}

func (r *redisTransactionRegistry) Status(key *Key) (TransactionStatus, error) {
	raw, err := r.redisClient.Get(context.Background(), toTransactionKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return TransactionStatus{State: transactionAbsent}, nil
	}

	if err != nil {
		log.Errorf("Failed to fetch transaction status from redis for key: %s", key.String())
		return TransactionStatus{State: transactionAbsent}, err
	}

	if len(raw) == 0 {
		log.Errorf("Failed to fetch transaction status from redis raw value: %s", key.String())
		return TransactionStatus{State: transactionAbsent}, err
	}

	state := TransactionState(uint8(raw[0]))
	var reason string
	if state.IsFailed() && len(raw) > 1 {
		reason = string(raw[1:])
	}
	return TransactionStatus{State: state, FailReason: reason}, nil
}

func (r *redisTransactionRegistry) Close() error {
	return r.redisClient.Close()
}

func toTransactionKey(key *Key) string {
	return fmt.Sprintf("%s-transaction", key.String())
}
