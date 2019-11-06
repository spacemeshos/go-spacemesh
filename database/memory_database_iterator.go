package database

import (
	"reflect"
)

type MemDatabaseIterator struct {
	keys  [][]byte
	db    map[string][]byte
	index int
}

func (iter *MemDatabaseIterator) Key() []byte {
	return iter.keys[iter.index]
}

func (iter *MemDatabaseIterator) Value() []byte {
	key := iter.keys[iter.index]

	return iter.db[string(key)]
}

func (iter *MemDatabaseIterator) Next() bool {
	if iter.index == len(iter.db)-1 {
		return false
	}

	iter.index++
	return true
}
func (iter *MemDatabaseIterator) First() bool {
	if len(iter.db) == 0 {
		iter.index = -1
		return false
	}

	iter.index = 0
	return true
}
func (iter *MemDatabaseIterator) Last() bool {
	size := len(iter.db)
	if size == 0 {
		iter.index = 0
		return false
	}

	iter.index = size - 1
	return true
}

func (iter *MemDatabaseIterator) Prev() bool {
	iter.index--
	if iter.index < 0 {
		iter.index = -1
		return false
	}

	return true
}

func (iter *MemDatabaseIterator) Seek(key []byte) bool {
	size := len(iter.db)
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

func (iter *MemDatabaseIterator) Release()     { return }
func (iter *MemDatabaseIterator) Error() error { return nil }
