package cache

import (
	"github.com/contentsquare/chproxy/log"
	"sync"
	"time"
)

type pendingEntry struct {
	deadline time.Time
}

type inMemoryTransactionRegistry struct {
	pendingEntriesLock sync.Mutex
	pendingEntries     map[*Key]pendingEntry

	graceTime time.Duration
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

func (i *inMemoryTransactionRegistry) Close() error {
	close(i.stopCh)
	i.wg.Wait()
	return nil
}

func newInMemoryTransactionRegistry(graceTime time.Duration) *inMemoryTransactionRegistry {
	transaction := &inMemoryTransactionRegistry{
		pendingEntriesLock: sync.Mutex{},
		pendingEntries:     make(map[*Key]pendingEntry),
		graceTime:          graceTime,
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

func (i *inMemoryTransactionRegistry) Unregister(key *Key) error {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	delete(i.pendingEntries, key)
	return nil
}

func (i *inMemoryTransactionRegistry) Register(key *Key) error {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	_, exists := i.pendingEntries[key]
	if !exists {
		i.pendingEntries[key] = pendingEntry{
			deadline: time.Now().Add(i.graceTime),
		}
	}
	return nil
}

func (i *inMemoryTransactionRegistry) IsDone(key *Key) bool {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	_, exists := i.pendingEntries[key]
	return !exists
}

func (i *inMemoryTransactionRegistry) pendingEntriesCleaner() {
	d := i.graceTime
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
