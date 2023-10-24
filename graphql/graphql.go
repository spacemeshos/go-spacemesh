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

// Package graphql provides a GraphQL interface to Ethereum node data.
package graphql

import (
	"context"
	"errors"
	"fmt"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"strconv"
	"strings"
	"sync"
)

var (
	errBlockInvariant = errors.New("block objects must be instantiated with at least one of num or hash")
)

type Long int64

// ImplementsGraphQLType returns true if Long implements the provided GraphQL type.
func (b Long) ImplementsGraphQLType(name string) bool { return name == "Long" }

// UnmarshalGraphQL unmarshals the provided GraphQL query data.
func (b *Long) UnmarshalGraphQL(input interface{}) error {
	var err error
	switch input := input.(type) {
	case string:
		// uncomment to support hex values
		if strings.HasPrefix(input, "0x") {
			// apply leniency and support hex representations of longs.
			value, err := hexutil.DecodeUint64(input)
			*b = Long(value)
			return err
		} else {
			value, err := strconv.ParseInt(input, 10, 64)
			*b = Long(value)
			return err
		}
	case int32:
		*b = Long(input)
	case int64:
		*b = Long(input)
	case float64:
		*b = Long(input)
	default:
		err = fmt.Errorf("unexpected type %T for Long", input)
	}
	return err
}

// Account represents a Spacemesh account at a particular layer.
type Account struct {
	r       *Resolver
	address types.Address
	layerID types.LayerID
}

// getAccount fetches the account object for an account given its address.
func (a *Account) getAccount(ctx context.Context) (*types.Account, error) {
	account, err := a.r.backend.AccountByLayer(ctx, a.layerID)
	return account, err
}

func (a *Account) Address(ctx context.Context) (types.Address, error) {
	return a.address, nil
}

func (a *Account) Balance(ctx context.Context) (uint64, error) {
	account, err := a.getAccount(ctx)
	if err != nil {
		return 0, err
	}
	return account.Balance, nil
}

// Transaction represents a Spacemesh transaction.
// backend and hash are mandatory; all others will be fetched when required.
type Transaction struct {
	r    *Resolver
	hash types.TransactionID // Must be present after initialization
	mu   sync.Mutex
	// mu protects following resources
	tx    *types.Transaction
	block *Block
	index uint64
}

// resolve returns the internal transaction object, fetching it if needed.
// It also returns the block the tx belongs to, unless it is a pending tx.
func (t *Transaction) resolve(ctx context.Context) (*types.Transaction, *Block) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tx != nil {
		return t.tx, t.block
	}
	// Try to return an already finalized transaction
	tx, blockHash, _, index, err := t.r.backend.GetTransaction(ctx, t.hash)
	if err == nil && tx != nil {
		t.tx = tx
		blockNrOrHash := api.BlockLayerOrHashWithHash(blockHash, false)
		t.block = &Block{
			r:           t.r,
			layerOrHash: &blockNrOrHash,
			hash:        blockHash,
		}
		t.index = index
		return t.tx, t.block
	}
	// No finalized transaction, try to retrieve it from the pool
	t.tx = t.r.backend.GetPoolTransaction(t.hash)
	return t.tx, nil
}

func (t *Transaction) Hash(ctx context.Context) common.Hash {
	return t.hash
}

func (t *Transaction) Nonce(ctx context.Context) hexutil.Uint64 {
	tx, _ := t.resolve(ctx)
	if tx == nil {
		return 0
	}
	return hexutil.Uint64(tx.Nonce())
}

func (t *Transaction) To(ctx context.Context, args BlockNumberArgs) *Account {
	tx, _ := t.resolve(ctx)
	if tx == nil {
		return nil
	}
	to := tx.To()
	if to == nil {
		return nil
	}
	return &Account{
		r:             t.r,
		address:       *to,
		blockNrOrHash: args.NumberOrLatest(),
	}
}

func (t *Transaction) From(ctx context.Context, args BlockNumberArgs) *Account {
	tx, _ := t.resolve(ctx)
	if tx == nil {
		return nil
	}
	signer := types.LatestSigner(t.r.backend.ChainConfig())
	from, _ := types.Sender(signer, tx)
	return &Account{
		r:             t.r,
		address:       from,
		blockNrOrHash: args.NumberOrLatest(),
	}
}

func (t *Transaction) Block(ctx context.Context) *Block {
	_, block := t.resolve(ctx)
	return block
}

func (t *Transaction) Index(ctx context.Context) *hexutil.Uint64 {
	_, block := t.resolve(ctx)
	// Pending tx
	if block == nil {
		return nil
	}
	index := hexutil.Uint64(t.index)
	return &index
}

func (t *Transaction) Status(ctx context.Context) (*hexutil.Uint64, error) {
	receipt, err := t.getReceipt(ctx)
	if err != nil || receipt == nil {
		return nil, err
	}
	if len(receipt.PostState) != 0 {
		return nil, nil
	}
	ret := hexutil.Uint64(receipt.Status)
	return &ret, nil
}

func (t *Transaction) GasUsed(ctx context.Context) (*hexutil.Uint64, error) {
	receipt, err := t.getReceipt(ctx)
	if err != nil || receipt == nil {
		return nil, err
	}
	ret := hexutil.Uint64(receipt.GasUsed)
	return &ret, nil
}

func (t *Transaction) CumulativeGasUsed(ctx context.Context) (*hexutil.Uint64, error) {
	receipt, err := t.getReceipt(ctx)
	if err != nil || receipt == nil {
		return nil, err
	}
	ret := hexutil.Uint64(receipt.CumulativeGasUsed)
	return &ret, nil
}

func (t *Transaction) Raw(ctx context.Context) (hexutil.Bytes, error) {
	tx, _ := t.resolve(ctx)
	if tx == nil {
		return hexutil.Bytes{}, nil
	}
	return tx.MarshalBinary()
}

type BlockType int

// Block represents a Spacemesh block.
// backend, and numberOrHash are mandatory. All other fields are lazily fetched
// when required.
type Block struct {
	r           *Resolver
	layerOrHash *api.BlockLayerOrHash // Field resolvers assume layerOrHash is always present
	mu          sync.Mutex
	// mu protects following resources
	hash  types.BlockID // Must be resolved during initialization
	block *types.Block
}

// resolve returns the internal Block object representing this block, fetching
// it if necessary.
func (b *Block) resolve(ctx context.Context) (*types.Block, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.block != nil {
		return b.block, nil
	}
	if b.layerOrHash == nil {
		latest := api.BlockLayerOrHashWithNumber(api.LatestBlockLayer)
		b.layerOrHash = &latest
	}
	var err error
	blocks, err := b.r.backend.BlocksByLayerOrHash(ctx, *b.layerOrHash)
	if blocks != nil {
		if len(blocks) == 1 {
			b.block = blocks[0]
			b.hash = b.block.ID()
		} else {
			err = fmt.Errorf("found multiple blocks for layer or hash")
		}
	}
	return b.block, err
}

func (b *Block) Layer(ctx context.Context) (types.LayerID, error) {
	block, err := b.resolve(ctx)
	if err != nil {
		return 0, err
	}

	return block.LayerIndex, nil
}

func (b *Block) Hash(ctx context.Context) (types.BlockID, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hash, nil
}

func (b *Block) GasLimit(ctx context.Context) (hexutil.Uint64, error) {
	header, err := b.resolveHeader(ctx)
	if err != nil {
		return 0, err
	}
	return hexutil.Uint64(header.GasLimit), nil
}

func (b *Block) GasUsed(ctx context.Context) (hexutil.Uint64, error) {
	header, err := b.resolveHeader(ctx)
	if err != nil {
		return 0, err
	}
	return hexutil.Uint64(header.GasUsed), nil
}

func (b *Block) Timestamp(ctx context.Context) (hexutil.Uint64, error) {
	header, err := b.resolveHeader(ctx)
	if err != nil {
		return 0, err
	}
	return hexutil.Uint64(header.Time), nil
}

func (b *Block) Nonce(ctx context.Context) (hexutil.Bytes, error) {
	header, err := b.resolveHeader(ctx)
	if err != nil {
		return hexutil.Bytes{}, err
	}
	return header.Nonce[:], nil
}

func (b *Block) StateRoot(ctx context.Context) (common.Hash, error) {
	header, err := b.resolveHeader(ctx)
	if err != nil {
		return common.Hash{}, err
	}
	return header.Root, nil
}

func (b *Block) RawHeader(ctx context.Context) (hexutil.Bytes, error) {
	header, err := b.resolveHeader(ctx)
	if err != nil {
		return hexutil.Bytes{}, err
	}
	return rlp.EncodeToBytes(header)
}

func (b *Block) Raw(ctx context.Context) (hexutil.Bytes, error) {
	block, err := b.resolve(ctx)
	if err != nil {
		return hexutil.Bytes{}, err
	}
	return rlp.EncodeToBytes(block)
}

// BlockNumberArgs encapsulates arguments to accessors that specify a block number.
type BlockNumberArgs struct {
	// TODO: Ideally we could use input unions to allow the query to specify the
	// block parameter by hash, block number, or tag but input unions aren't part of the
	// standard GraphQL schema SDL yet, see: https://github.com/graphql/graphql-spec/issues/488
	Block *Long
}

// NumberOr returns the provided block number argument, or the "current" block number or hash if none
// was provided.
func (a BlockNumberArgs) NumberOr(current rpc.BlockNumberOrHash) rpc.BlockNumberOrHash {
	if a.Block != nil {
		blockNr := rpc.BlockNumber(*a.Block)
		return rpc.BlockNumberOrHashWithNumber(blockNr)
	}
	return current
}

// NumberOrLatest returns the provided block number argument, or the "latest" block number if none
// was provided.
func (a BlockNumberArgs) NumberOrLatest() rpc.BlockNumberOrHash {
	return a.NumberOr(rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber))
}

func (b *Block) TransactionCount(ctx context.Context) (*hexutil.Uint64, error) {
	block, err := b.resolve(ctx)
	if err != nil || block == nil {
		return nil, err
	}
	count := hexutil.Uint64(len(block.Transactions()))
	return &count, err
}

func (b *Block) Transactions(ctx context.Context) (*[]*Transaction, error) {
	block, err := b.resolve(ctx)
	if err != nil || block == nil {
		return nil, err
	}
	ret := make([]*Transaction, 0, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		ret = append(ret, &Transaction{
			r:     b.r,
			hash:  tx.Hash(),
			tx:    tx,
			block: b,
			index: uint64(i),
		})
	}
	return &ret, nil
}

func (b *Block) TransactionAt(ctx context.Context, args struct{ Index Long }) (*Transaction, error) {
	block, err := b.resolve(ctx)
	if err != nil || block == nil {
		return nil, err
	}
	txs := block.Transactions()
	if args.Index < 0 || int(args.Index) >= len(txs) {
		return nil, nil
	}
	tx := txs[args.Index]
	return &Transaction{
		r:     b.r,
		hash:  tx.Hash(),
		tx:    tx,
		block: b,
		index: uint64(args.Index),
	}, nil
}

func (b *Block) Account(ctx context.Context, args struct {
	Address common.Address
}) (*Account, error) {
	return &Account{
		r:             b.r,
		address:       args.Address,
		blockNrOrHash: *b.numberOrHash,
	}, nil
}

type Pending struct {
	r *Resolver
}

func (p *Pending) TransactionCount(ctx context.Context) (hexutil.Uint64, error) {
	txs, err := p.r.backend.GetPoolTransactions()
	return hexutil.Uint64(len(txs)), err
}

func (p *Pending) Transactions(ctx context.Context) (*[]*Transaction, error) {
	txs, err := p.r.backend.GetPoolTransactions()
	if err != nil {
		return nil, err
	}
	ret := make([]*Transaction, 0, len(txs))
	for i, tx := range txs {
		ret = append(ret, &Transaction{
			r:     p.r,
			hash:  tx.Hash(),
			tx:    tx,
			index: uint64(i),
		})
	}
	return &ret, nil
}

func (p *Pending) Account(ctx context.Context, args struct {
	Address common.Address
}) *Account {
	pendingBlockNr := rpc.BlockNumberOrHashWithNumber(rpc.PendingBlockNumber)
	return &Account{
		r:             p.r,
		address:       args.Address,
		blockNrOrHash: pendingBlockNr,
	}
}

// Resolver is the top-level object in the GraphQL hierarchy.
type Resolver struct {
	backend api.Backend
}

func (r *Resolver) Block(ctx context.Context, args struct {
	Number *Long
	Hash   *common.Hash
}) (*Block, error) {
	if args.Number != nil && args.Hash != nil {
		return nil, errors.New("only one of number or hash must be specified")
	}
	var numberOrHash rpc.BlockNumberOrHash
	if args.Number != nil {
		if *args.Number < 0 {
			return nil, nil
		}
		number := rpc.BlockNumber(*args.Number)
		numberOrHash = rpc.BlockNumberOrHashWithNumber(number)
	} else if args.Hash != nil {
		numberOrHash = rpc.BlockNumberOrHashWithHash(*args.Hash, false)
	} else {
		numberOrHash = rpc.BlockNumberOrHashWithNumber(rpc.LatestBlockNumber)
	}
	block := &Block{
		r:            r,
		numberOrHash: &numberOrHash,
	}
	// Resolve the header, return nil if it doesn't exist.
	// Note we don't resolve block directly here since it will require an
	// additional network request for light client.
	h, err := block.resolveHeader(ctx)
	if err != nil {
		return nil, err
	} else if h == nil {
		return nil, nil
	}
	return block, nil
}

func (r *Resolver) Blocks(ctx context.Context, args struct {
	From *Long
	To   *Long
}) ([]*Block, error) {
	from := rpc.BlockNumber(*args.From)

	var to rpc.BlockNumber
	if args.To != nil {
		to = rpc.BlockNumber(*args.To)
	} else {
		to = rpc.BlockNumber(r.backend.CurrentBlock().Number.Int64())
	}
	if to < from {
		return []*Block{}, nil
	}
	var ret []*Block
	for i := from; i <= to; i++ {
		numberOrHash := rpc.BlockNumberOrHashWithNumber(i)
		block := &Block{
			r:            r,
			numberOrHash: &numberOrHash,
		}
		// Resolve the header to check for existence.
		// Note we don't resolve block directly here since it will require an
		// additional network request for light client.
		h, err := block.resolveHeader(ctx)
		if err != nil {
			return nil, err
		} else if h == nil {
			// Blocks after must be non-existent too, break.
			break
		}
		ret = append(ret, block)
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func (r *Resolver) Pending(ctx context.Context) *Pending {
	return &Pending{r}
}

func (r *Resolver) Transaction(ctx context.Context, args struct{ Hash common.Hash }) *Transaction {
	tx := &Transaction{
		r:    r,
		hash: args.Hash,
	}
	// Resolve the transaction; if it doesn't exist, return nil.
	t, _ := tx.resolve(ctx)
	if t == nil {
		return nil
	}
	return tx
}

func (r *Resolver) SendRawTransaction(ctx context.Context, args struct{ Data hexutil.Bytes }) (common.Hash, error) {
	tx := new(types.Transaction)
	if err := tx.UnmarshalBinary(args.Data); err != nil {
		return common.Hash{}, err
	}
	hash, err := ethapi.SubmitTransaction(ctx, r.backend, tx)
	return hash, err
}

func (r *Resolver) GasPrice(ctx context.Context) (hexutil.Big, error) {
	tipcap, err := r.backend.SuggestGasTipCap(ctx)
	if err != nil {
		return hexutil.Big{}, err
	}
	if head := r.backend.CurrentHeader(); head.BaseFee != nil {
		tipcap.Add(tipcap, head.BaseFee)
	}
	return (hexutil.Big)(*tipcap), nil
}

func (r *Resolver) GenesisID(ctx context.Context) (hexutil.Big, error) {
	return hexutil.Big(*r.backend.ChainConfig().ChainID), nil
}

// SyncState represents the synchronisation status returned from the `syncing` accessor.
type SyncState struct {
	progress ethereum.SyncProgress
}

func (s *SyncState) StartingBlock() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.StartingBlock)
}
func (s *SyncState) CurrentBlock() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.CurrentBlock)
}
func (s *SyncState) HighestBlock() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.HighestBlock)
}
func (s *SyncState) SyncedAccounts() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.SyncedAccounts)
}
func (s *SyncState) SyncedAccountBytes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.SyncedAccountBytes)
}
func (s *SyncState) SyncedBytecodes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.SyncedBytecodes)
}
func (s *SyncState) SyncedBytecodeBytes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.SyncedBytecodeBytes)
}
func (s *SyncState) SyncedStorage() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.SyncedStorage)
}
func (s *SyncState) SyncedStorageBytes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.SyncedStorageBytes)
}
func (s *SyncState) HealedTrienodes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.HealedTrienodes)
}
func (s *SyncState) HealedTrienodeBytes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.HealedTrienodeBytes)
}
func (s *SyncState) HealedBytecodes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.HealedBytecodes)
}
func (s *SyncState) HealedBytecodeBytes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.HealedBytecodeBytes)
}
func (s *SyncState) HealingTrienodes() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.HealingTrienodes)
}
func (s *SyncState) HealingBytecode() hexutil.Uint64 {
	return hexutil.Uint64(s.progress.HealingBytecode)
}

// Syncing returns false in case the node is currently not syncing with the network. It can be up-to-date or has not
// yet received the latest block headers from its pears. In case it is synchronizing:
// - startingBlock:       block number this node started to synchronize from
// - currentBlock:        block number this node is currently importing
// - highestBlock:        block number of the highest block header this node has received from peers
// - syncedAccounts:      number of accounts downloaded
// - syncedAccountBytes:  number of account trie bytes persisted to disk
// - syncedBytecodes:     number of bytecodes downloaded
// - syncedBytecodeBytes: number of bytecode bytes downloaded
// - syncedStorage:       number of storage slots downloaded
// - syncedStorageBytes:  number of storage trie bytes persisted to disk
// - healedTrienodes:     number of state trie nodes downloaded
// - healedTrienodeBytes: number of state trie bytes persisted to disk
// - healedBytecodes:     number of bytecodes downloaded
// - healedBytecodeBytes: number of bytecodes persisted to disk
// - healingTrienodes:    number of state trie nodes pending
// - healingBytecode:     number of bytecodes pending
func (r *Resolver) Syncing() (*SyncState, error) {
	progress := r.backend.SyncProgress()

	// Return not syncing if the synchronisation already completed
	if progress.CurrentBlock >= progress.HighestBlock {
		return nil, nil
	}
	// Otherwise gather the block sync stats
	return &SyncState{progress}, nil
}
