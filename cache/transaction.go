package cache

import "io"

type Transaction interface {
	io.Closer

	Register(key *Key) error
	Unregister(key *Key) error
	IsDone(key *Key) bool
}

type NoOpTransaction struct {

}

func (n *NoOpTransaction) Close() error {
	return nil
}

func (n *NoOpTransaction) Register(key *Key) error {
	return nil
}

func (n *NoOpTransaction) Unregister(key *Key) error {
	return nil
}

func (n *NoOpTransaction) IsDone(key *Key) bool {
	return true
}
