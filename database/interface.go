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

type ContextDBCreator struct {
	Create  func(file string, cache int, handles int, logger log.Log) (Database, error)
	Path    string
	Context string
}

func (c ContextDBCreator) CreateRealDB(file string, cache int, handles int, logger log.Log) (Database, error) {
	return NewLDBDatabase(c.Path+c.Context+file, cache, handles, logger)
}

func (c ContextDBCreator) CreateMemDB(file string, cache int, handles int, logger log.Log) (Database, error) {
	return NewMemDatabase(), nil
}

var DBC = ContextDBCreator{
	Path:    "../tmp/test/" + time.Now().String(),
	Context: "",
}

var Create = DBC.CreateRealDB

func SwitchCreationContext(path, context string) {
	var c = ContextDBCreator{
		Path:    path,
		Context: context,
	}
	Create = c.CreateRealDB
}

func SwitchToMemCreationContext() {
	var c = ContextDBCreator{
	}
	Create = c.CreateMemDB
}

