package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
)

// Version must be increased with each backward-incompatible change
// in the cache storage.
const Version = 5

// Key is the key for use in the cache.
type Key struct {
	// Query must contain full request query.
	Query []byte

	// AcceptEncoding must contain 'Accept-Encoding' request header value.
	AcceptEncoding string

	// ClientProtocolVersion must contain `client_protocol_version` query arg.
	ClientProtocolVersion string

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

	// Version represents data encoding version number
	Version int

	// QueryParamsHash must contain hashed value of query params
	QueryParamsHash uint32

	// UserCredentialHash must contain hashed value of username & password
	UserCredentialHash uint32
}

// NewKey construct cache key from provided parameters with default version number
func NewKey(query []byte, originParams url.Values, acceptEncoding string, userParamsHash uint32, queryParamsHash uint32, userCredentialHash uint32) *Key {
	return &Key{
		Query:                 query,
		AcceptEncoding:        acceptEncoding,
		ClientProtocolVersion: originParams.Get("client_protocol_version"),
		DefaultFormat:         originParams.Get("default_format"),
		Database:              originParams.Get("database"),
		Compress:              originParams.Get("compress"),
		EnableHTTPCompression: originParams.Get("enable_http_compression"),
		Namespace:             originParams.Get("cache_namespace"),
		Extremes:              originParams.Get("extremes"),
		MaxResultRows:         originParams.Get("max_result_rows"),
		ResultOverflowMode:    originParams.Get("result_overflow_mode"),
		UserParamsHash:        userParamsHash,
		Version:               Version,
		QueryParamsHash:       queryParamsHash,
		UserCredentialHash:    userCredentialHash,
	}
}

func (k *Key) filePath(dir string) string {
	return filepath.Join(dir, k.String())
}

// String returns string representation of the key.
func (k *Key) String() string {
	s := fmt.Sprintf("V%d; Query=%q; AcceptEncoding=%q; ClientProtocolVersion=%q; DefaultFormat=%q; Database=%q; Compress=%q; EnableHTTPCompression=%q; Namespace=%q; MaxResultRows=%q; Extremes=%q; ResultOverflowMode=%q; UserParams=%d; QueryParams=%d; UserCredentialHash=%d",
		k.Version, k.Query, k.AcceptEncoding, k.ClientProtocolVersion, k.DefaultFormat, k.Database, k.Compress, k.EnableHTTPCompression, k.Namespace,
		k.MaxResultRows, k.Extremes, k.ResultOverflowMode, k.UserParamsHash, k.QueryParamsHash, k.UserCredentialHash)
	h := sha256.Sum256([]byte(s))

	// The first 16 bytes of the hash should be enough
	// for collision prevention :)
	return hex.EncodeToString(h[:16])
}
