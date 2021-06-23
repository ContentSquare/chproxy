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
	fsCache, err := NewFSCache(cfg)

	if err != nil {
		panic("can not instanciate file system cache")
	}

	return &AsyncCache{
		Cache: fsCache,
		Transaction: NewInMemoryTransaction(10 * time.Second), // todo set duration from config
	}
}
