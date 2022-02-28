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
	pendingEntries     map[*Key]pendingEntry

	deadline time.Duration
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func newInMemoryTransactionRegistry(deadline time.Duration) *inMemoryTransactionRegistry {
	transaction := &inMemoryTransactionRegistry{
		pendingEntriesLock: sync.Mutex{},
		pendingEntries:     make(map[*Key]pendingEntry),
		deadline: deadline,
		stopCh:   make(chan struct{}),
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
	_, exists := i.pendingEntries[key]
	if !exists {
		i.pendingEntries[key] = pendingEntry{
			deadline: time.Now().Add(i.deadline),
			state:    transactionCreated,
		}
	}
	return nil
}

func (i *inMemoryTransactionRegistry) Complete(key *Key) error {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	if pendingEntry, ok := i.pendingEntries[key]; ok {
		pendingEntry.state = transactionCompleted
		i.pendingEntries[key] = pendingEntry
	} else {
		// todo: should we register an entry in that case anyway (I guess)
		log.Errorf("attempt to complete transaction failed, because entry not found for key: %s", key.String())
	}
	return nil
}

func (i *inMemoryTransactionRegistry) Fail(key *Key) error {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	if pendingEntry, ok := i.pendingEntries[key]; ok {
		pendingEntry.state = transactionFailed
		i.pendingEntries[key] = pendingEntry
	} else {
		// todo: should we register an entry in that case anyway (I guess)
		log.Errorf("attempt to complete transaction failed, because entry not found for key: %s", key.String())
	}
	return nil
}

func (i *inMemoryTransactionRegistry) Status(key *Key) (TransactionState, error) {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	if entry, ok := i.pendingEntries[key]; ok {
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
