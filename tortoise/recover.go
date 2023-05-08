package tortoise

import (
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Recover tortoise state from database.
func Recover(db *datastore.CachedDB, beacon system.BeaconGetter, opts ...Opt) (*Tortoise, error) {
	trtl, err := New(opts...)
	if err != nil {
		return nil, err
	}
	return trtl, nil
}
