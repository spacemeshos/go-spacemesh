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
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/syndtr/goleveldb/leveldb/iterator"
	"path/filepath"
	"time"
)

// IdealBatchSize is the best batch size
// Code using batches should try to add this much data to the batch.
// The value was determined empirically.
const IdealBatchSize = 100 * 1024

// Putter wraps the database write operation supported by both batches and regular databases.
type Putter interface {
	Put(key []byte, value []byte) error
}

// Getter wraps the database read operation.
type Getter interface {
	Get(key []byte) ([]byte, error)
}

// Deleter wraps the database delete operation supported by both batches and regular databases.
type Deleter interface {
	Delete(key []byte) error
}

// Database wraps all database operations. All methods are safe for concurrent use.
type Database interface {
	Putter
	Deleter
	Get(key []byte) ([]byte, error)
	Has(key []byte) (bool, error)
	Close()
	NewBatch() Batch
	Find(key []byte) Iterator
}

// Store wraps basic get and put operations, All methods are safe for concurrent use.
type Store interface {
	Putter
	Getter
}

// Batch is a write-only database that commits changes to its host database
// when Write is called. Batch cannot be used concurrently.
type Batch interface {
	Putter
	Deleter
	ValueSize() int // amount of data in the batch
	Write() error
	// Reset resets the batch for reuse
	Reset()
}

// Iterator defined basic iterator interface
type Iterator interface {
	iterator.IteratorSeeker
	Key() []byte
	Value() []byte
}

// ContextDBCreator is a global structure that toggles creation of real dbs and memory dbs for tests
type ContextDBCreator struct {
	Create  func(file string, cache int, handles int, logger log.Log) (Database, error)
	Path    string
	Context string
}

// CreateRealDB is a wrapper function that creates a leveldb database
func (c ContextDBCreator) CreateRealDB(file string, cache int, handles int, logger log.Log) (Database, error) {
	return NewLDBDatabase(filepath.Join(c.Path+c.Context, file), cache, handles, logger)
}

// CreateMemDB is a wrapper function that creates a memory database to be used only in tests
func (c ContextDBCreator) CreateMemDB(file string, cache int, handles int, logger log.Log) (Database, error) {
	return NewMemDatabase(), nil
}

// DBC is the global context that determines what db will be created and at what path.
var DBC = ContextDBCreator{
	Path:    "../tmp/test/" + time.Now().String(),
	Context: "",
}

// Create is the actual function that is used to create a DB without the need to know which DB specifically,
// this can be determined by the context
var Create = DBC.CreateRealDB

// SwitchCreationContext switches real DB creation context to allow creating the same DB for multiple nodes in one process
func SwitchCreationContext(path, context string) {
	var c = ContextDBCreator{
		Path:    path,
		Context: context,
	}
	Create = c.CreateRealDB
}

// SwitchToMemCreationContext switches global DB creation function to create memory DBs (this funciton should be called in
// tests 'init' function
func SwitchToMemCreationContext() {
	var c = ContextDBCreator{}
	Create = c.CreateMemDB
}
