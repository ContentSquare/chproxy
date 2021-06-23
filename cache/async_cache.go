package cache

import "github.com/Vertamedia/chproxy/cache/transaction"

type AsyncCache struct {
	Cache
	transaction.Repository
}

func NewAsyncCache() *AsyncCache {
	return &AsyncCache{}
}
