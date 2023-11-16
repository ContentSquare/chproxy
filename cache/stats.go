package cache

// Stats represents cache stats
type Stats struct {
	// Size is the cache size in bytes.
	Size uint64

	// Items is the number of items in the cache.
	Items uint64
}
