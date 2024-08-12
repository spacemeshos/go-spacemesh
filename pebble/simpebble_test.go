package pebble_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/spacemeshos/go-spacemesh/pebble"
)

func TestFlat(t *testing.T) {
	dir := t.TempDir()
	pdb := pebble.New(dir)
	defer pdb.Close()
	k, v := []byte("key"), []byte("value")
	if err := pdb.Put(k, v); err != nil {
		t.Fatal(err)
	}

	val, err := pdb.Get(k)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(val, v) {
		t.Fatal(val)
	}
}

func TestIterator(t *testing.T) {
	dir := t.TempDir()
	pdb := pebble.New(dir)
	defer pdb.Close()
	var keys, vals [][]byte
	for i := 250; i < 260; i++ {
		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, uint32(i))
		keys = append(keys, key)
		vals = append(vals, []byte{byte(i)})
	}
	for i, k := range keys {
		if err := pdb.Put(k, vals[i]); err != nil {
			t.Fatal(err)
		}
	}
	for i, k := range keys {
		val, err := pdb.Get(k)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(val, vals[i]) {
			t.Fatal(val)
		}
	}
	pfx := []byte{0, 0, 0}
	iterated := 0
	pdb.IterPrefix(pfx, func(k, v []byte) bool {
		fmt.Println(k, v)
		iterated++
		return false
	})
	if iterated != 6 {
		t.Fatalf("wrong number of iterated prefixes: %d", iterated)
	}
}

func TestDeleteRange(t *testing.T) {
	dir := t.TempDir()
	pdb := pebble.New(dir)
	defer pdb.Close()
	var keys, vals [][]byte
	for i := 250; i < 260; i++ {
		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, uint32(i))
		keys = append(keys, key)
		vals = append(vals, []byte{byte(i)})
	}
	for i, k := range keys {
		if err := pdb.Put(k, vals[i]); err != nil {
			t.Fatal(err)
		}
	}
	for i, k := range keys {
		val, err := pdb.Get(k)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(val, vals[i]) {
			t.Fatal(val)
		}
	}
	pfx := []byte{0, 0, 0}
	pfx2 := []byte{0, 0, 1}
	iterated := 0
	if err := pdb.DeleteRange(pfx, pfx2); err != nil {
		t.Fatal(err)
	}
	pdb.IterPrefix(pfx, func(k, v []byte) bool {
		fmt.Println(k, v)
		iterated++
		return false
	})
	if iterated != 0 {
		t.Fatalf("wrong number of iterated prefixes: %d", iterated)
	}
	pfx[2] += 1
	pdb.IterPrefix(pfx, func(k, v []byte) bool {
		fmt.Println(k, v)
		iterated++
		return false
	})
	if iterated != 4 {
		t.Fatalf("wrong number of iterated prefixes: %d", iterated)
	}
}

func TestDeletePrefix(t *testing.T) {
	dir := t.TempDir()
	pdb := pebble.New(dir)
	defer pdb.Close()
	var keys, vals [][]byte
	for i := 250; i < 260; i++ {
		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, uint32(i))
		keys = append(keys, key)
		vals = append(vals, []byte{byte(i)})
	}
	for i, k := range keys {
		if err := pdb.Put(k, vals[i]); err != nil {
			t.Fatal(err)
		}
	}
	for i, k := range keys {
		val, err := pdb.Get(k)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(val, vals[i]) {
			t.Fatal(val)
		}
	}
	pfx := []byte{0, 0, 0}
	iterated := 0
	if err := pdb.DeletePrefix(pfx); err != nil {
		t.Fatal(err)
	}
	pdb.IterPrefix(pfx, func(k, v []byte) bool {
		fmt.Println(k, v)
		iterated++
		return false
	})
	if iterated != 0 {
		t.Fatalf("wrong number of iterated prefixes: %d", iterated)
	}
	pfx[2] += 1
	pdb.IterPrefix(pfx, func(k, v []byte) bool {
		fmt.Println(k, v)
		iterated++
		return false
	})
	if iterated != 4 {
		t.Fatalf("wrong number of iterated prefixes: %d", iterated)
	}
}

func TestHas(t *testing.T) {
	dir := t.TempDir()
	pdb := pebble.New(dir)
	defer pdb.Close()
	var keys, vals [][]byte
	for i := 250; i < 260; i++ {
		key := make([]byte, 4)
		binary.BigEndian.PutUint32(key, uint32(i))
		keys = append(keys, key)
		vals = append(vals, []byte{byte(i)})
	}
	for i, k := range keys {
		if err := pdb.Put(k, vals[i]); err != nil {
			t.Fatal(err)
		}
	}
	for i, k := range keys {
		val, err := pdb.Get(k)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(val, vals[i]) {
			t.Fatal(val)
		}
	}
	key := keys[3]
	h, err := pdb.Has(key)
	if err != nil {
		t.Fatal(err)
	}
	if !h {
		t.Fatal("entry missing")
	}

	key = []byte{0xff, 0xff, 0xff, 0xff}
	h, err = pdb.Has(key)
	if err != nil {
		t.Fatal(err)
	}
	if h {
		t.Fatal("entry found when shouldn't")
	}

	key = []byte{0, 0} // prefix exists, but key is incomplete
	h, err = pdb.Has(key)
	if err != nil {
		t.Fatal(err)
	}
	if h {
		t.Fatal("entry found when shouldn't")
	}
}
