package transaction

import "github.com/Vertamedia/chproxy/cache"

type Repository interface {
	Register(key *cache.Key) error
	Unregister(key *cache.Key) error
	IsDone(key *cache.Key) bool
}
