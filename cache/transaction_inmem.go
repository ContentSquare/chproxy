package cache

import (
	"sync"
	"time"
)

type pendingEntry struct {
	deadline time.Time
}

type InMemoryTransaction struct {
	pendingEntriesLock sync.Mutex
	pendingEntries     map[*Key]pendingEntry

	graceTime time.Duration
	stopCh    chan struct{}
}

// todo check l'histoire du pending entries cleaner
func NewInMemoryTransaction(graceTime time.Duration) *InMemoryTransaction {
	inMemoryTransaction := &InMemoryTransaction{
		pendingEntriesLock: sync.Mutex{},
		pendingEntries:     make(map[*Key]pendingEntry),
		graceTime:          graceTime,
		stopCh:             make(chan struct{}),
	}

	go inMemoryTransaction.pendingEntriesCleaner()

	return inMemoryTransaction
}

func (i *InMemoryTransaction) Unregister(key *Key) error {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	delete(i.pendingEntries, key)
	return nil
}

func (i *InMemoryTransaction) Register(key *Key) error {
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

func (i *InMemoryTransaction) IsDone(key *Key) bool {
	i.pendingEntriesLock.Lock()
	defer i.pendingEntriesLock.Unlock()
	_, exists := i.pendingEntries[key]
	return !exists
}

func (i *InMemoryTransaction) pendingEntriesCleaner() {
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
