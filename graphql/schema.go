// Copyright 2019 The go-ethereum Authors
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

package graphql

const schema string = `
    # Bytes32 is a 32 byte binary string, represented as 0x-prefixed hexadecimal.
    scalar Bytes32
    # Address is a 20 byte address, represented as 0x-prefixed hexadecimal.
    scalar Address
    # Bytes is an arbitrary length binary string, represented as 0x-prefixed hexadecimal.
    # An empty byte string is represented as '0x'. Byte strings must have an even number of hexadecimal nybbles.
    scalar Bytes
    # BigInt is a large integer. Input is accepted as either a JSON number or as a string.
    # Strings may be either decimal or 0x-prefixed hexadecimal. Output values are all
    # 0x-prefixed hexadecimal.
    scalar BigInt
    # Long is a 64 bit unsigned integer. Input is accepted as either a JSON number or as a string.
    # Strings may be either decimal or 0x-prefixed hexadecimal. Output values are all
    # 0x-prefixed hexadecimal.
    scalar Long

    schema {
        query: Query
        mutation: Mutation
    }

    # Account is an account at a particular block.
    type Account {
        # Address is the address owning the account.
        address: Address!
        # Balance is the balance of the account, in wei.
        balance: BigInt!
        # TransactionCount is the number of transactions sent from this account,
        # or in the case of a contract, the number of contracts created. Otherwise
        # known as the nonce.
        transactionCount: Long!
    }

    # Transaction is a transaction.
    type Transaction {
        # Hash is the hash of this transaction.
        hash: Bytes32!
        # Nonce is the nonce of the account this transaction was generated with.
        nonce: Long!
        # Index is the index of this transaction in the parent block. This will
        # be null if the transaction has not yet been mined.
        index: Long
        # From is the account that sent this transaction - this will always be
        # an externally owned account.
        from(block: Long): Account!
        # To is the account the transaction was sent to. This is null for
        # contract-creating transactions.
        to(block: Long): Account
        # Block is the block this transaction was mined in. This will be null if
        # the transaction has not yet been mined.
        block: Block

        # Status is the return status of the transaction. This will be 1 if the
        # transaction succeeded, or 0 if it failed (due to a revert, or due to
        # running out of gas). If the transaction has not yet been mined, this
        # field will be null.
        status: Long
        # GasUsed is the amount of gas that was used processing this transaction.
        # If the transaction has not yet been mined, this field will be null.
        gasUsed: Long
        # Raw is the canonical encoding of the transaction.
        raw: Bytes!
    }

    # Block is a block.
    type Block {
        # Number is the number of this block, starting at 0 for the genesis block.
        number: Long!
        # Hash is the block hash of this block.
        hash: Bytes32!
        # TransactionCount is the number of transactions in this block. if
        # transactions are not available for this block, this field will be null.
        transactionCount: Long
        # StateRoot is the keccak256 hash of the state trie after this block was processed.
        stateRoot: Bytes32!
        # GasLimit is the maximum amount of gas that was available to transactions in this block.
        gasLimit: Long!
        # GasUsed is the amount of gas that was used executing transactions in this block.
        gasUsed: Long!
        # Timestamp is the unix timestamp at which this block was mined.
        timestamp: Long!
        # Transactions is a list of transactions associated with this block. If
        # transactions are unavailable for this block, this field will be null.
        transactions: [Transaction!]
        # TransactionAt returns the transaction at the specified index. If
        # transactions are unavailable for this block, or if the index is out of
        # bounds, this field will be null.
        transactionAt(index: Long!): Transaction
        # Account fetches an account at the current block's state.
        account(address: Address!): Account!
        # RawHeader is the RLP encoding of the block's header.
        rawHeader: Bytes!
        # Raw is the RLP encoding of the block.
        raw: Bytes!
    }

    # SyncState contains the current synchronisation state of the client.
    type SyncState {
        # StartingBlock is the block number at which synchronisation started.
        startingBlock: Long!
        # CurrentBlock is the point at which synchronisation has presently reached.
        currentBlock: Long!
        # HighestBlock is the latest known block number.
        highestBlock: Long!
    }

    # Pending represents the current pending state.
    type Pending {
        # TransactionCount is the number of transactions in the pending state.
        transactionCount: Long!
        # Transactions is a list of transactions in the current pending state.
        transactions: [Transaction!]
        # Account fetches an account for the pending state.
        account(address: Address!): Account!
    }

    type Query {
        # Block fetches a block by number or by hash. If neither is
        # supplied, the most recent known block is returned.
        block(number: Long, hash: Bytes32): Block
        # Blocks returns all the blocks between two numbers, inclusive. If
        # to is not supplied, it defaults to the most recent known block.
        blocks(from: Long, to: Long): [Block!]!
        # Pending returns the current pending state.
        pending: Pending!
        # Transaction returns a transaction specified by its hash.
        transaction(hash: Bytes32!): Transaction
        # Syncing returns information on the current synchronisation state.
        syncing: SyncState
        # GenesisID returns the current genesis ID for transaction replay protection.
        genesisID: BigInt!
    }

    type Mutation {
        # SendRawTransaction sends an encoded transaction to the network.
        sendRawTransaction(data: Bytes!): Bytes32!
    }
`
