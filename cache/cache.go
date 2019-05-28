package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/contentsquare/chproxy/config"
	"net/http"
	"time"
)

// cacheVersion must be increased with each backwads-incompatible change
// in the cache storage.
const cacheVersion = 2

type Cache interface {
	WriteTo(rw http.ResponseWriter, key *Key) error
	Close()
	NewResponseWriter(rw http.ResponseWriter, key *Key) (ResponseWriter, error)
	clean()
	Stats() Stats
	MaxSize() uint64
}

func NewCache(cfg config.Cache) (cache *Cache, err error) {
	if cfg.RedisHost != "" {
		return NewRedisCache(cfg)
	}
	return NewFileCache(cfg)
}

type pendingEntry struct {
	deadline time.Time
}

// Stats represents cache stats
type Stats struct {
	// Size is the cache size in bytes.
	Size uint64

	// Items is the number of items in the cache.
	Items uint64
}

// Key is the key for use in the cache.
type Key struct {
	// Query must contain full request query.
	Query []byte

	// AcceptEncoding must contain 'Accept-Encoding' request header value.
	AcceptEncoding string

	// DefaultFormat must contain `default_format` query arg.
	DefaultFormat string

	// Database must contain `database` query arg.
	Database string

	// Compress must contain `compress` query arg.
	Compress string

	// EnableHTTPCompression must contain `enable_http_compression` query arg.
	EnableHTTPCompression string

	// Namespace is an optional cache namespace.
	Namespace string

	// MaxResultRows must contain `max_result_rows` query arg
	MaxResultRows string

	// Extremes must contain `extremes` query arg
	Extremes string

	// ResultOverflowMode must contain `result_overflow_mode` query arg
	ResultOverflowMode string

	// UserParamsHash must contain hashed value of users params
	UserParamsHash uint32
}

// String returns string representation of the key.
func (k *Key) String() string {
	s := fmt.Sprintf("V%d; Query=%q; AcceptEncoding=%q; DefaultFormat=%q; Database=%q; Compress=%q; EnableHTTPCompression=%q; Namespace=%q; MaxResultRows=%q; Extremes=%q; ResultOverflowMode=%q; UserParams=%d",
		cacheVersion, k.Query, k.AcceptEncoding, k.DefaultFormat, k.Database, k.Compress, k.EnableHTTPCompression, k.Namespace,
		k.MaxResultRows, k.Extremes, k.ResultOverflowMode, k.UserParamsHash)
	h := sha256.Sum256([]byte(s))

	// The first 16 bytes of the hash should be enough
	// for collision prevention :)
	return hex.EncodeToString(h[:16])
}
