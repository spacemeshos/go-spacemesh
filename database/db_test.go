package database

import (
	"flag"
	"fmt"
	"github.com/stretchr/testify/assert"
	"os"
	"testing"
)

func TestDB_reopendatabase(t *testing.T) {
	db := NewLevelDbStore("test", nil, nil)
	key := []byte("some key")
	db.Put(key, []byte("wonderful"))
	str, _ := db.Get(key)
	fmt.Println(string(str))
	db.Close()
	db2 := NewLevelDbStore("test", nil, nil)
	_, err := db2.Get(key)
	assert.True(t, err == nil, "wrong layer")
	db2.Close()
}

func TestDB_reopendatabase2(t *testing.T) {
	db := NewLevelDbStore("test", nil, nil)
	str, _ := db.Get([]byte("some key"))
	fmt.Println(string(str))
	db.Close()
}

func TestDB_delete(t *testing.T) {
	key := []byte("some key")
	db := NewLevelDbStore("test", nil, nil)
	db.Put(key, []byte("wonderful"))
	str, err := db.Get(key)
	fmt.Println(string(str))
	assert.True(t, err == nil, "wrong layer")
	db.Delete(key)
	str, err = db.Get(key)
	fmt.Println(string(str))
	assert.True(t, err != nil, "wrong layer")
}

func TestMain(m *testing.M) {
	flag.Parse()
	res := m.Run()
	cleanup()
	os.Exit(res)
}

func cleanup() {
	_ = os.RemoveAll("test")
}
