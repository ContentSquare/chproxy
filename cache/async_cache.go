package cache

import (
	"github.com/Vertamedia/chproxy/config"
	"time"
)

type AsyncCache struct {
	Cache
	Transaction
}

func NewAsyncCache(cfg config.Cache) *AsyncCache {
	graceTime := time.Duration(cfg.GraceTime)
	if graceTime == 0 {
		// Default grace time.
		graceTime = 5 * time.Second
	}
	if graceTime < 0 {
		// Disable protection from `dogpile effect`.
		graceTime = 0
	}

	fsCache, err := newFSCache(cfg, graceTime)

	if err != nil {
		panic("can not instanciate file system cache")
	}

	return &AsyncCache{
		Cache:       fsCache,
		Transaction: newInMemoryTransaction(graceTime),
	}
}
