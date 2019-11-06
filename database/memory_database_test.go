package database

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func checkRow(key []byte, value []byte, iter *MemDatabaseIterator, t *testing.T) {
	assert.Equal(t, key, iter.Key(), fmt.Sprintf("have key : %s, need: %s", iter.Key(), key))
	assert.Equal(t, value, iter.Value(), fmt.Sprintf("have value : %s, need: %s", iter.Value(), value))
}

func TestMemoryDB_Iterator(t *testing.T) {
	firstKey := []byte("first key")
	secondKey := []byte("second key")
	firstValue := []byte("first value")
	secondValue := []byte("second value")

	db := NewMemDatabase()
	db.Put(firstKey, firstValue)
	db.Put(secondKey, secondValue)

	iter := db.NewMemDatabaseIterator()
	iter.Next()
	checkRow(firstKey, firstValue, iter, t)
	iter.Next()
	checkRow(secondKey, secondValue, iter, t)
}
