package tortoisebeacon

import (
	"fmt"
	"sync"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
)

// DB holds beacons for epochs.
type DB struct {
	sync.RWMutex
	store database.Database
	log   log.Log
}

func NewDB(dbStore database.Database, log log.Log) *DB {
	db := &DB{
		store: dbStore,
		log:   log,
	}

	return db
}

func (db *DB) GetTortoiseBeacon(epochID types.EpochID) (types.Hash32, bool) {
	id, err := db.store.Get(epochID.ToBytes())
	if err != nil {
		return types.Hash32{}, false
	}

	return types.BytesToHash(id), true
}

func (db *DB) SetTortoiseBeacon(epochID types.EpochID, beacon types.Hash32) error {
	db.log.Info("added tortoise beacon for epoch %v", epochID)

	err := db.store.Put(epochID.ToBytes(), beacon.Bytes())
	if err != nil {
		return fmt.Errorf("failed to add tortoise beacon: %v", err)
	}

	return nil
}
