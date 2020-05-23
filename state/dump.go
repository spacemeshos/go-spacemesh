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
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/rlp"
	"github.com/spacemeshos/go-spacemesh/trie"
)

// DumpAccount is a helper struct that helps dumping account balance and nonce in json form
type DumpAccount struct {
	Balance string `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

// Dump is a struct used to dump an entire state root into json form
type Dump struct {
	Root     string                 `json:"root"`
	Accounts map[string]DumpAccount `json:"accounts"`
}

// RawDump returns a Dump struct for the receivers state
func (state *DB) RawDump() Dump {
	dump := Dump{
		Root:     fmt.Sprintf("%x", state.globalTrie.Hash()),
		Accounts: make(map[string]DumpAccount),
	}

	it := trie.NewIterator(state.globalTrie.NodeIterator(nil))
	for it.Next() {
		addr := state.globalTrie.GetKey(it.Key)
		var data Account
		if err := rlp.DecodeBytes(it.Value, &data); err != nil {
			panic(err)
		}

		//obj := newObject(nil, address.BytesToAddress(addr), data)
		account := DumpAccount{
			Balance: data.Balance.String(),
			Nonce:   data.Nonce,
		}

		dump.Accounts[util.Bytes2Hex(addr)] = account
	}
	return dump
}

// Dump dumps the current state into json form, encoded into bytes.
func (state *DB) Dump() []byte {
	json, err := json.MarshalIndent(state.RawDump(), "", "	")
	if err != nil {
		fmt.Println("dump err", err)
	}

	return json
}
