package cache

import (
	"sync"
	"time"

	"github.com/contentsquare/chproxy/log"
)

type pendingEntry struct {
	deadline time.Time
	state    TransactionState
}

type inMemoryTransactionRegistry struct {
	pendingEntriesLock sync.Mutex
	pendingEntries     map[string]pendingEntry

	deadline time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func newInMemoryTransactionRegistry(deadline time.Duration) *inMemoryTransactionRegistry {
	transaction := &inMemoryTransactionRegistry{
		pendingEntriesLock: sync.Mutex{},
		pendingEntries:     make(map[string]pendingEntry),
		deadline:           deadline,
		stopCh:             make(chan struct{}),
	}

	transaction.wg.Add(1)
	go func() {
		log.Debugf("inmem transaction: cleaner start")
		transaction.pendingEntriesCleaner()
		transaction.wg.Done()
		log.Debugf("inmem transaction: cleaner stop")
	}()

	return transaction
}

func (i *inMemoryTransactionRegistry) Create(key *Key) error {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	k := key.String()
	_, exists := i.pendingEntries[k]
	if !exists {
		i.pendingEntries[k] = pendingEntry{
			deadline: time.Now().Add(i.deadline),
			state:    transactionCreated,
		}
	}
	return nil
}

func (i *inMemoryTransactionRegistry) Complete(key *Key) error {
	i.updateTransactionState(key, transactionCompleted)
	return nil
}

func (i *inMemoryTransactionRegistry) Fail(key *Key) error {
	i.updateTransactionState(key, transactionFailed)
	return nil
}

func (i *inMemoryTransactionRegistry) updateTransactionState(key *Key, state TransactionState) {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	k := key.String()
	if entry, ok := i.pendingEntries[k]; ok {
		entry.state = state
		i.pendingEntries[k] = entry
	} else {
		log.Errorf("[attempt to complete transaction] entry not found for key: %s, registering new entry with %v status", key.String(), state)
		i.pendingEntries[k] = pendingEntry{
			deadline: time.Now().Add(i.deadline),
			state:    state,
		}
	}
}

func (i *inMemoryTransactionRegistry) Status(key *Key) (TransactionState, error) {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	k := key.String()
	if entry, ok := i.pendingEntries[k]; ok {
		return entry.state, nil
	}
	return transactionCompleted, ErrMissingTransaction
}

func (i *inMemoryTransactionRegistry) Close() error {
	close(i.stopCh)
	i.wg.Wait()
	return nil
}

func (i *inMemoryTransactionRegistry) pendingEntriesCleaner() {
	d := i.deadline
	if d < 100*time.Millisecond {
		d = 100 * time.Millisecond
	}
	if d > time.Second {
		d = time.Second
	}

	for {
		currentTime := time.Now()

		// Clear outdated pending entries, since they may remain here
		// forever if unregisterPendingEntry call is missing.
		i.pendingEntriesLock.Lock()
		for path, pe := range i.pendingEntries {
			if currentTime.After(pe.deadline) {
				delete(i.pendingEntries, path)
			}
		}
		i.pendingEntriesLock.Unlock()

		select {
		case <-time.After(d):
		case <-i.stopCh:
			return
		}
	}
}
