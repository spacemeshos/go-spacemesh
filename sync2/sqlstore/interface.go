package sqlstore

import (
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
)

// IDStore represents a store of IDs (keys).
type IDStore interface {
	// Clone makes a copy of the store.
	// It is expected to be an O(1) operation.
	Clone() IDStore
	// Release releases the resources associated with the store.
	Release()
	// RegisterKey registers the key with the store.
	RegisterKey(k rangesync.KeyBytes) error
	// All returns all keys in the store.
	All() rangesync.SeqResult
	// From returns all keys in the store starting from the given key.
	// sizeHint is a hint for the expected number of keys to be returned.
	From(from rangesync.KeyBytes, sizeHint int) rangesync.SeqResult
}
