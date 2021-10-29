package cache

import (
	"errors"
	"io"
	"time"
)

// Cache stores results of executed queries identified by Key
type Cache interface {
	io.Closer
	// TODO consider the value of Stats in future iterations. Maybe it is not needed?
	Stats() Stats
	Get(key *Key) (*CachedData, error)
	Put(r io.ReadSeeker, key *Key) (time.Duration, error)
	Name() string
}

type CachedData struct {
	Data io.ReadSeeker
	Ttl  time.Duration
}

// ErrMissing is returned when the entry isn't found in the cache.
var ErrMissing = errors.New("missing cache entry")
