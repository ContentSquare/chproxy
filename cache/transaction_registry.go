package cache

import (
	"io"
	"time"
)

// TransactionRegistry is a registry of ongoing queries identified by Key.
type TransactionRegistry interface {
	io.Closer

	// Create creates a new transaction record
	Create(key *Key) error

	// Complete completes a transaction for given key
	Complete(key *Key) error

	// Fail fails a transaction for given key
	Fail(key *Key, reason string) error

	// Status checks the status of the transaction
	Status(key *Key) (TransactionStatus, error)
}

// transactionEndedTTL amount of time transaction record is kept after being updated
const transactionEndedTTL = 500 * time.Millisecond

type TransactionStatus struct {
	State      TransactionState
	FailReason string // filled in only if state of transaction is transactionFailed
}

type TransactionState uint8

const (
	transactionCreated   TransactionState = 0
	transactionCompleted TransactionState = 1
	transactionFailed    TransactionState = 2
	transactionAbsent    TransactionState = 3
)

func (t *TransactionState) IsAbsent() bool {
	if t != nil {
		return *t == transactionAbsent
	}
	return false
}

func (t *TransactionState) IsFailed() bool {
	if t != nil {
		return *t == transactionFailed
	}
	return false
}

func (t *TransactionState) IsCompleted() bool {
	if t != nil {
		return *t == transactionCompleted
	}
	return false
}

func (t *TransactionState) IsPending() bool {
	if t != nil {
		return *t == transactionCreated
	}
	return false
}
