// Copyright 2016 The go-ethereum Authors
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

package state

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/stretchr/testify/require"

	"testing"
	//check "gopkg.in/check.v1"
)

// Tests that updating a state trie does not leak any database writes prior to
// actually committing the state.
func TestUpdateLeaks(t *testing.T) {
	// Create an empty state database
	db := database.NewMemDatabase()
	state, _ := New(types.Hash32{}, NewDatabase(db))

	// Update it with some accounts
	for i := byte(0); i < 255; i++ {
		addr := types.BytesToAddress([]byte{i})
		state.AddBalance(addr, uint64(11*i))
		state.SetNonce(addr, uint64(42*i))
		state.IntermediateRoot(false)
	}
	// Ensure that no data was leaked into the database
	iter := db.Find(nil)
	defer iter.Release()
	for iter.Next() {
		require.FailNowf(t, "leaked key", "%x", iter.Key())
	}
}

// Tests that no intermediate state of an object is stored into the database,
// only the one right before the commit.
func TestIntermediateLeaks(t *testing.T) {
	// Create two state databases, one transitioning to the final state, the other final from the beginning
	transDb := database.NewMemDatabase()
	finalDb := database.NewMemDatabase()
	transState, _ := New(types.Hash32{}, NewDatabase(transDb))
	finalState, _ := New(types.Hash32{}, NewDatabase(finalDb))

	modify := func(state *DB, addr types.Address, i, tweak byte) {
		state.SetBalance(addr, uint64(int64(11*i)+int64(tweak)))
		state.SetNonce(addr, uint64(42*i+tweak))
	}

	// Modify the transient state.
	for i := byte(0); i < 255; i++ {
		modify(transState, types.Address{byte(i)}, i, 0)
	}
	// Write modifications to trie.
	transState.IntermediateRoot(false)

	// Overwrite all the data with new values in the transient database.
	for i := byte(0); i < 255; i++ {
		modify(transState, types.Address{byte(i)}, i, 99)
		modify(finalState, types.Address{byte(i)}, i, 99)
	}

	// Commit and cross check the databases.
	if _, err := transState.Commit(); err != nil {
		t.Fatalf("failed to commit transition state: %v", err)
	}
	if _, err := finalState.Commit(); err != nil {
		t.Fatalf("failed to commit final state: %v", err)
	}
	it := finalDb.Find(nil)
	defer it.Release()
	for it.Next() {
		key := it.Key()
		if _, err := transDb.Get(key); err != nil {
			val, _ := finalDb.Get(key)
			t.Errorf("entry missing from the transition database: %x -> %x", key, val)
		}
	}
	it = transDb.Find(nil)
	defer it.Release()
	for it.Next() {
		key := it.Key()
		if _, err := finalDb.Get(key); err != nil {
			val, _ := transDb.Get(key)
			t.Errorf("extra entry in the transition database: %x -> %x", key, val)
		}
	}
}

// TestCopy tests that copying a statedb object indeed makes the original and
// the copy independent of each other. This test is a regression test against
// https://github.com/ethereum/go-ethereum/pull/15549.
func TestCopy(t *testing.T) {
	// Create a random state test to copy and modify "independently"
	orig, _ := New(types.Hash32{}, NewDatabase(database.NewMemDatabase()))

	for i := byte(0); i < 255; i++ {
		obj := orig.GetOrNewStateObj(types.BytesToAddress([]byte{i}))
		obj.AddBalance(uint64(i))
		orig.updateStateObj(obj)
	}

	// Copy the state, modify both in-memory
	copy := orig.Copy()

	for i := byte(0); i < 255; i++ {
		origObj := orig.GetOrNewStateObj(types.BytesToAddress([]byte{i}))
		copyObj := copy.GetOrNewStateObj(types.BytesToAddress([]byte{i}))

		origObj.AddBalance(2 * uint64(i))
		copyObj.AddBalance(3 * uint64(i))

		orig.updateStateObj(origObj)
		copy.updateStateObj(copyObj)
	}
	// Finalise the changes on both concurrently
	done := make(chan struct{})
	go func() {
		close(done)
	}()
	<-done

	// Verify that the two states have been updated independently
	for i := byte(0); i < 255; i++ {
		origObj := orig.GetOrNewStateObj(types.BytesToAddress([]byte{i}))
		copyObj := copy.GetOrNewStateObj(types.BytesToAddress([]byte{i}))

		if want := (3 * uint64(i)); origObj.Balance() != want {
			t.Errorf("orig obj %d: balance mismatch: have %v, want %v", i, origObj.Balance(), want)
		}
		if want := (4 * uint64(i)); copyObj.Balance() != want {
			t.Errorf("copy obj %d: balance mismatch: have %v, want %v", i, copyObj.Balance(), want)
		}
	}
}

// TestCopyOfCopy tests that modified objects are carried over to the copy, and the copy of the copy.
// See https://github.com/ethereum/go-ethereum/pull/15225#issuecomment-380191512
func TestCopyOfCopy(t *testing.T) {
	sdb, _ := New(types.Hash32{}, NewDatabase(database.NewMemDatabase()))
	addr := types.HexToAddress("aaaa")
	sdb.SetBalance(addr, 42)

	if got := sdb.Copy().GetBalance(addr); got != 42 {
		t.Fatalf("1st copy fail, expected 42, got %v", got)
	}
	if got := sdb.Copy().Copy().GetBalance(addr); got != 42 {
		t.Fatalf("2nd copy fail, expected 42, got %v", got)
	}
}
