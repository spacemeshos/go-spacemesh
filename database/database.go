// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package database defines interfaces to key value type databases used by various components in go-spacemesh node
package database

import (
	"fmt"
	"sync"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/filter"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"

	"github.com/spacemeshos/go-spacemesh/log"
)

// ErrNotFound is special type error for not found in DB.
var ErrNotFound = errors.ErrNotFound

// LDBDatabase  is a wrapper for leveldb database with concurrent access.
type LDBDatabase struct {
	fn string      // filename for reporting
	db *leveldb.DB // LevelDB instance

	quitLock sync.Mutex      // Mutex protecting the quit channel access
	quitChan chan chan error // Quit channel to stop the metrics collection before closing the database

	log log.Log // Contextual logger tracking the database path
}

// NewLDBDatabase returns a LevelDB wrapped object.
func NewLDBDatabase(file string, cache int, handles int, logger log.Log) (*LDBDatabase, error) {
	// Ensure we have some minimal caching and file guarantees
	if cache < 16 {
		cache = 16
	}
	if handles < 16 {
		handles = 16
	}
	logger.With().Info("allocated cache and file handles",
		log.Int("cache_size", cache),
		log.Int("num_handles", handles))

	// Open the db and recover any potential corruptions
	db, err := leveldb.OpenFile(file, &opt.Options{
		OpenFilesCacheCapacity: handles,
		BlockCacheCapacity:     cache / 2 * opt.MiB,
		WriteBuffer:            cache / 4 * opt.MiB, // Two of these are used internally
		Filter:                 filter.NewBloomFilter(10),
	})
	if _, corrupted := err.(*errors.ErrCorrupted); corrupted {
		db, err = leveldb.RecoverFile(file, nil)
	}
	// (Re)check for errors and abort if opening of the db failed
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}
	return &LDBDatabase{
		fn:  file,
		db:  db,
		log: logger,
	}, nil
}

// Path returns the path to the database directory.
func (db *LDBDatabase) Path() string {
	return db.fn
}

// Put puts the given key / value to the queue.
func (db *LDBDatabase) Put(key []byte, value []byte) error {
	if err := db.db.Put(key, value, nil); err != nil {
		return fmt.Errorf("put value: %w", err)
	}

	return nil
}

// Has returns whether the db contains the key.
func (db *LDBDatabase) Has(key []byte) (bool, error) {
	has, err := db.db.Has(key, nil)
	if err != nil {
		return false, fmt.Errorf("check value: %w", err)
	}

	return has, nil
}

// Get returns the given key if it's present.
func (db *LDBDatabase) Get(key []byte) ([]byte, error) {
	dat, err := db.db.Get(key, nil)
	if err != nil {
		return nil, fmt.Errorf("get value: %w", err)
	}
	return dat, nil
}

// Delete deletes the key from the queue and database.
func (db *LDBDatabase) Delete(key []byte) error {
	if err := db.db.Delete(key, nil); err != nil {
		return fmt.Errorf("delete value: %w", err)
	}

	return nil
}

// Close closes database, flushing writes and denying all new write requests.
func (db *LDBDatabase) Close() {
	// Stop the metrics collection to avoid internal database races
	db.quitLock.Lock()
	defer db.quitLock.Unlock()

	if db.quitChan != nil {
		errc := make(chan error)
		db.quitChan <- errc
		if err := <-errc; err != nil {
			db.log.With().Error("metrics collection failed", log.Err(err))
		}
		db.quitChan = nil
	}
	if err := db.db.Close(); err != nil {
		db.log.With().Error("failed to close database", log.String("file", db.fn), log.Err(err))
	} else {
		db.log.With().Info("database closed", log.String("file", db.fn))
	}
}

// NewMemDatabase returns a memory database instance.
func NewMemDatabase() *LDBDatabase {
	backend := storage.NewMemStorage()
	db, err := leveldb.Open(backend, nil)
	if err != nil {
		panic("can't open in-memory leveldb: " + err.Error())
	}
	return &LDBDatabase{db: db, log: log.NewNop()}
}
