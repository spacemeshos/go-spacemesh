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
	"encoding/json"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"github.com/spacemeshos/go-spacemesh/trie"
)

// RawDump returns a Dump struct for the receivers state
func (state *DB) RawDump() types.MultipleAccountsState {
	// Reading the state root and accounts data here is concurrency safe since this
	// method should only be called after a lock has been acquired on state
	dump := types.MultipleAccountsState{
		Root:     fmt.Sprintf("%x", state.globalTrie.Hash()),
		Accounts: make(map[string]types.AccountState),
	}

	it := trie.NewIterator(state.globalTrie.NodeIterator(nil))
	for it.Next() {
		addr := state.globalTrie.GetKey(it.Key)
		var state types.AccountState
		if err := rlp.DecodeBytes(it.Value, &state); err != nil {
			panic(err)
		}
		dump.Accounts[util.Bytes2Hex(addr)] = state
	}
	return dump
}

// Dump dumps the current state into json form, encoded into bytes.
func (state *DB) Dump() []byte {
	stateJSON, err := json.MarshalIndent(state.RawDump(), "", "	")
	if err != nil {
		fmt.Println("dump err", err)
	}

	return stateJSON
}
