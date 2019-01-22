package database

import (
	"reflect"
	"testing"
)

func checkRow(key []byte,value []byte , iter *MemDatabaseIterator, t *testing.T) {
	if (reflect.DeepEqual(key, iter.Key()) == false || reflect.DeepEqual(value, iter.Value()) == false) {
		t.Fatalf("Key/Value doesnt match iterator state")
	}
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
