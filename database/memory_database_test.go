package database

import (
	"fmt"
	"reflect"
	"testing"
)

func checkRow(key []byte,value []byte , iter *MemDatabaseIterator, t *testing.T) {
	fmt.Println("key", key)
	fmt.Println("ter.Key()", iter.Key())
	fmt.Println("value", value)
	fmt.Println("iter.Value(", iter.Value())
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
