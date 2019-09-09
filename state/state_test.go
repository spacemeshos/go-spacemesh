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

package state

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
	//	checker "gopkg.in/check.v1"
)

type StateSuite struct {
	db    *database.MemDatabase
	state *StateDB
}

var toAddr = types.BytesToAddress

func TestDump(t *testing.T) {
	s := &StateSuite{}
	s.db = database.NewMemDatabase()
	s.state, _ = New(types.Hash32{}, NewDatabase(s.db))
	// generate a few entries
	obj1 := s.state.GetOrNewStateObj(toAddr([]byte{0x01}))
	obj1.AddBalance(big.NewInt(22))
	obj2 := s.state.GetOrNewStateObj(toAddr([]byte{0x01, 0x02}))
	obj2.SetNonce(10)
	obj3 := s.state.GetOrNewStateObj(toAddr([]byte{0x02}))
	obj3.SetBalance(big.NewInt(44))

	// write some of them to the trie
	s.state.updateStateObj(obj1)
	s.state.updateStateObj(obj2)
	s.state.Commit(false)

	// check that dump contains the state objects that are in trie
	got := string(s.state.Dump())
	want := `{
	"root": "ba94994b7d4b6590b615f0a8ab543445312fd303fdab013f0b0fba920f8f228b",
	"accounts": {
		"0000000000000000000000000000000000000001": {
			"balance": "22",
			"nonce": 0
		},
		"0000000000000000000000000000000000000002": {
			"balance": "44",
			"nonce": 0
		},
		"0000000000000000000000000000000000000102": {
			"balance": "0",
			"nonce": 10
		}
	}
}`
	if got != want {
		t.Errorf("dump mismatch:\ngot: %s\nwant: %s\n", got, want)
	}
}

func TestLookupPastState(t *testing.T) {
	s := &StateSuite{}
	s.db = database.NewMemDatabase()
	sdb := NewDatabase(s.db)
	s.state, _ = New(types.Hash32{}, sdb)
	// generate a few entries
	obj1 := s.state.GetOrNewStateObj(toAddr([]byte{0x01}))
	obj1.AddBalance(big.NewInt(22))

	oldState, err := s.state.Commit(false)
	assert.NoError(t, err)

	obj1.AddBalance(big.NewInt(10))
	_, err = s.state.Commit(false)
	assert.NoError(t, err)

	oldSt, err := New(oldState, sdb)
	assert.NoError(t, err)
	assert.Equal(t, oldSt.GetBalance(toAddr([]byte{0x01})), uint64(22))
	assert.Equal(t, s.state.GetBalance(toAddr([]byte{0x01})), uint64(32))

}

func (s *StateSuite) SetUpTest(t *testing.T) {
	s.db = database.NewMemDatabase()
	s.state, _ = New(types.Hash32{}, NewDatabase(s.db))
}

func compareStateObjects(so0, so1 *StateObj, t *testing.T) {
	if so0.Address() != so1.Address() {
		t.Fatalf("Address mismatch: have %v, want %v", so0.address, so1.address)
	}
	if so0.Balance().Cmp(so1.Balance()) != 0 {
		t.Fatalf("Balance mismatch: have %v, want %v", so0.Balance(), so1.Balance())
	}
	if so0.Nonce() != so1.Nonce() {
		t.Fatalf("Nonce mismatch: have %v, want %v", so0.Nonce(), so1.Nonce())
	}

}
