package cache

import "io"

// TransactionRegistry is a registry of ongoing queries identified by Key.
type TransactionRegistry interface {
	io.Closer

	// Register creates a new transaction record
	Register(key *Key) error
	// Unregister removes a transaction record
	Unregister(key *Key) error
	// IsDone checks the status of a transaction
	IsDone(key *Key) bool
}
