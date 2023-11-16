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
	Put(r io.Reader, ctMetadata ContentMetadata, key *Key) (time.Duration, error)
	Name() string
}

type ContentMetadata struct {
	Length   int64
	Type     string
	Encoding string
}

type CachedData struct {
	ContentMetadata
	Data io.ReadCloser // we need a ReadCloser because the reader is used oustide the scope where it was created and need to be closed by the function using it.
	Ttl  time.Duration
}

// ErrMissing is returned when the entry isn't found in the cache.
var ErrMissing = errors.New("missing cache entry")
