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
	inMemoryTransaction := newInMemoryTransactionRegistry(graceTime, graceTime)

	if err := inMemoryTransaction.Create(key); err != nil {
		t.Fatalf("unexpected error: %s while registering new transaction", err)
	}

	status, err := inMemoryTransaction.Status(key)
	if err != nil || !status.State.IsPending() {
		t.Fatalf("unexpected: transaction should be pending")
	}

	if err := inMemoryTransaction.Complete(key); err != nil {
		t.Fatalf("unexpected error: %s while unregistering transaction", err)
	}

	status, err = inMemoryTransaction.Status(key)
	if err != nil || !status.State.IsCompleted() {
		t.Fatalf("unexpected: transaction should be done")
	}

}
