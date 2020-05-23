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

package database

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"sort"
	"sync"
)

// MemDatabase is a test memory database. Do not use for any production it does not get persisted
type MemDatabase struct {
	db   map[string][]byte
	lock sync.RWMutex
}

// NewMemDatabase returns a memory database instance
func NewMemDatabase() *MemDatabase {
	return &MemDatabase{
		db: make(map[string][]byte),
	}
}

// Put inserts value value by provided key
func (db *MemDatabase) Put(key []byte, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.db[string(key)] = util.CopyBytes(value)
	return nil
}

// Has returns a boolean if key is in db or not
func (db *MemDatabase) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	_, ok := db.db[string(key)]
	return ok, nil
}

// Get gets the value for the given key, returns an error if key wasn't found
func (db *MemDatabase) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if entry, ok := db.db[string(key)]; ok {
		return util.CopyBytes(entry), nil
	}
	return nil, ErrNotFound
}

// Keys returns all keys found in database
func (db *MemDatabase) Keys() [][]byte {
	db.lock.RLock()
	defer db.lock.RUnlock()

	keys := [][]byte{}
	for key := range db.db {
		keys = append(keys, []byte(key))
	}
	return keys
}

// Delete removes the key from db
func (db *MemDatabase) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	delete(db.db, string(key))
	return nil
}

// Close closes the database
func (db *MemDatabase) Close() {}

// NewBatch returns batch object to aggregate writes to db
func (db *MemDatabase) NewBatch() Batch {
	return &memBatch{db: db}
}

// Len returns number of items in database
func (db *MemDatabase) Len() int { return len(db.db) }

// NewMemDatabaseIterator  iterator for memory database iterating all items in database
func (db *MemDatabase) NewMemDatabaseIterator() *MemDatabaseIterator {
	keys := make([][]byte, 0, len(db.db))
	for k := range db.db {
		keys = append(keys, []byte(k))
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})
	return &MemDatabaseIterator{
		keys:  keys,
		db:    db.db,
		index: -1,
	}
}

// Iterator returns iterator for memory database iterating all items in database
func (db *MemDatabase) Iterator() Iterator {
	return db.NewMemDatabaseIterator()
}

// Find returns iterator iterating items with given key as prefix
func (db *MemDatabase) Find(key []byte) Iterator {
	keys := make([][]byte, 0, len(db.db))
	for k := range db.db {
		if bytes.HasPrefix([]byte(k), key) {
			keys = append(keys, []byte(k))
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		return string(keys[i]) < string(keys[j])
	})
	return &MemDatabaseIterator{
		keys:  keys,
		db:    db.db,
		index: -1,
	}
}

type kv struct {
	k, v []byte
	del  bool
}

type memBatch struct {
	db     *MemDatabase
	writes []kv
	size   int
}

func (b *memBatch) Put(key, value []byte) error {
	b.writes = append(b.writes, kv{util.CopyBytes(key), util.CopyBytes(value), false})
	b.size += len(value)
	return nil
}

func (b *memBatch) Delete(key []byte) error {
	b.writes = append(b.writes, kv{util.CopyBytes(key), nil, true})
	b.size++
	return nil
}

func (b *memBatch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	for _, kv := range b.writes {
		if kv.del {
			delete(b.db.db, string(kv.k))
			continue
		}
		b.db.db[string(kv.k)] = kv.v
	}
	return nil
}

func (b *memBatch) ValueSize() int {
	return b.size
}

func (b *memBatch) Reset() {
	b.writes = b.writes[:0]
	b.size = 0
}
