package cache

import (
	"testing"
	"time"
)

func TestInMemoryTransaction(t *testing.T) {
	graceTime := 10 * time.Second
	key := &Key{
		Query: []byte("SELECT pending entries"),
	}
	inMemoryTransaction := newInMemoryTransactionRegistry(graceTime)

	if err := inMemoryTransaction.Register(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	isDone := inMemoryTransaction.IsDone(key)
	if isDone {
		t.Fatalf("unexpected: transaction should be pending")
	}

	if err := inMemoryTransaction.Unregister(key); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	isDone = inMemoryTransaction.IsDone(key)
	if !isDone {
		t.Fatalf("unexpected: transaction should be done")
	}

}
