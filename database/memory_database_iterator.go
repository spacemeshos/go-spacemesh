package database

import (
	"reflect"
)

// MemDatabaseIterator is an iterator for memory database
type MemDatabaseIterator struct {
	keys  [][]byte
	db    map[string][]byte
	index int
}

// Key returns the key of the current item iterator is pointing at
func (iter *MemDatabaseIterator) Key() []byte {
	return iter.keys[iter.index]
}

// Value returns the value of the current item iterator is pointing at
func (iter *MemDatabaseIterator) Value() []byte {
	key := iter.keys[iter.index]

	return iter.db[string(key)]
}

// Next advances iterator to next item
func (iter *MemDatabaseIterator) Next() bool {
	if iter.index == len(iter.keys)-1 {
		return false
	}

	iter.index++
	return true
}

// First moves the iterator to first object
func (iter *MemDatabaseIterator) First() bool {
	if len(iter.db) == 0 {
		iter.index = -1
		return false
	}

	iter.index = 0
	return true
}

// Last moves the iterator to last object
func (iter *MemDatabaseIterator) Last() bool {
	size := len(iter.keys)
	if size == 0 {
		iter.index = 0
		return false
	}

	iter.index = size - 1
	return true
}

// Prev moves the iterator one item back
func (iter *MemDatabaseIterator) Prev() bool {
	iter.index--
	if iter.index < 0 {
		iter.index = -1
		return false
	}

	return true
}

// Seek returns true if key is found in iterator object
func (iter *MemDatabaseIterator) Seek(key []byte) bool {
	size := len(iter.keys)
	if size == 0 {
		iter.index = 0
		return false
	}
	for k := range iter.db {
		if reflect.DeepEqual(k, key) {
			return true
		}
	}

	return false
}

// Release is a stub to comply with DB interface
func (iter *MemDatabaseIterator) Release() { return }

// Error is a stub to comply with DB interface
func (iter *MemDatabaseIterator) Error() error { return nil }
