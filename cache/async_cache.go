package cache

type AsyncCache struct {
	Cache
	Transaction
}

func NewAsyncCache() *AsyncCache {
	return &AsyncCache{}
}
