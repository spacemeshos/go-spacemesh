package database

import (
	"fmt"
	"testing"
)

func TestDB_reopendatabase(t *testing.T) {
	db := NewLevelDbStore("test", nil, nil)
	key := []byte("some fucking key")
	db.Put(key, []byte("fucking wonderful"))
	str, _ := db.Get(key)
	fmt.Println(string(str))
	db.Close()
	db2 := NewLevelDbStore("test", nil, nil)
	str2, _ := db2.Get(key)
	fmt.Println(string(str2))
	db2.Close()
}

func TestDB_reopendatabase2(t *testing.T) {
	db2 := NewLevelDbStore("test", nil, nil)
	str2, _ := db2.Get([]byte("some fucking key"))
	fmt.Println(string(str2))
	db2.Close()
}
