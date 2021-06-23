package transaction

import (
	"sync"
	"time"
	"github.com/Vertamedia/chproxy/cache"
)

type pendingEntry struct {
	deadline time.Time
}

type InMemoryTransaction struct {
	pendingEntriesLock sync.Mutex
	pendingEntries     map[*cache.Key]pendingEntry
}

func (i *InMemoryTransaction) Unregister(key *cache.Key) error {
	panic("implement me")
}

func (i *InMemoryTransaction) Register(key *cache.Key) error {
	panic("implement me")
}

func (i *InMemoryTransaction) IsDone(key *cache.Key) bool {
	panic("implement me")
}

func (i *InMemoryTransaction) registerPendingEntry(key *cache.Key) bool {
	if i.graceTime <= 0 {
		return true
	}

	c.pendingEntriesLock.Lock()
	_, exists := c.pendingEntries[path]
	if !exists {
		c.pendingEntries[path] = pendingEntry{
			deadline: time.Now().Add(c.graceTime),
		}
	}
	c.pendingEntriesLock.Unlock()
	return !exists
}

func (i *InMemoryTransaction) unregisterPendingEntry(path string) {
	if c.graceTime <= 0 {
		return
	}

	c.pendingEntriesLock.Lock()
	delete(c.pendingEntries, path)
	c.pendingEntriesLock.Unlock()
}

