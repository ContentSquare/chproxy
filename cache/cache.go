package cache

import (
	"errors"
	"io"
	"net/http"
	"time"
)

type Cache interface {
	io.Closer
	Stats() Stats
	Get(w http.ResponseWriter, key *Key) error
	Put(r io.Reader, key *Key) (time.Duration, error)
	Name() string
}

// ErrMissing is returned when the entry isn't found in the cache.
var ErrMissing = errors.New("missing cache entry")