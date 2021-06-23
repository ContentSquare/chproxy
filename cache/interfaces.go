package cache

import (
	"io"
	"net/http"
	"os"
)

type Cache interface {
	io.Closer
	Stats() Stats
	Get(w http.ResponseWriter, key *Key) error
	Put(r *os.File, key *Key) error
	Name() string
}

