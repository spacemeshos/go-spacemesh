package vm

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
	"github.com/stretchr/testify/require"
)

func genTx(tb testing.TB, signer *signing.EdSigner, receiver types.Address, nonce, amount, fee uint64) *types.Transaction {
	tb.Helper()
	tx, err := transaction.GenerateCallTransaction(signer, receiver, nonce, amount, 0, fee)
	require.NoError(tb, err)
	return tx
}

type step struct {
	txs     []*types.Transaction
	failed  []int
	rewards []types.AnyReward

	revert uint32 // number of the layer
	state  map[types.Address]uint64
}

func TestLayers(t *testing.T) {
	rng := rand.New(rand.NewSource(101))
	signers := make([]*signing.EdSigner, 10)
	addresses := make([]types.Address, len(signers))
	for i := range signers {
		signer := signing.NewEdSignerFromRand(rng)
		signers[i] = signer
		addresses[i] = types.BytesToAddress(signer.PublicKey().Bytes())
	}

	for _, tc := range []struct {
		desc    string
		genesis map[string]uint64
		steps   []step
	}{
		{
			desc: "Rewards",
			steps: []step{
				{
					rewards: []types.AnyReward{
						{Address: addresses[0], Amount: 100},
						{Address: addresses[1], Amount: 200},
					},
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 200,
					},
				},
				{
					txs: []*types.Transaction{
						genTx(t, signers[0], addresses[2], 1, 50, 10),
						genTx(t, signers[1], addresses[2], 1, 50, 10),
					},
					state: map[types.Address]uint64{
						addresses[0]: 40,
						addresses[1]: 140,
						addresses[2]: 100,
					},
				},
			},
		},
		{
			desc: "Genesis",
			genesis: map[string]uint64{
				addresses[0].Hex(): 100,
				addresses[1].Hex(): 100,
			},
			steps: []step{
				{
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 100,
					},
				},
				{
					txs: []*types.Transaction{
						genTx(t, signers[1], addresses[0], 1, 90, 10),
					},
					state: map[types.Address]uint64{
						addresses[0]: 190,
						addresses[1]: 0,
					},
				},
			},
		},
		{
			desc: "Revert",
			genesis: map[string]uint64{
				addresses[0].Hex(): 100,
				addresses[1].Hex(): 100,
			},
			steps: []step{
				{
					txs: []*types.Transaction{
						genTx(t, signers[1], addresses[2], 1, 80, 10),
					},
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 10,
						addresses[2]: 80,
					},
				},
				{
					rewards: []types.AnyReward{
						{Address: addresses[2], Amount: 100},
						{Address: addresses[3], Amount: 100},
					},
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 10,
						addresses[2]: 180,
						addresses[3]: 100,
					},
				},
				{
					revert: 1,
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 10,
						addresses[2]: 80,
					},
				},
			},
		},
		{
			desc: "Invalid",
			genesis: map[string]uint64{
				addresses[0].Hex(): 1000,
				addresses[1].Hex(): 1000,
			},
			steps: []step{
				{
					txs: []*types.Transaction{
						genTx(t, signers[0], addresses[3], 1, 100, 0),
						genTx(t, signers[0], addresses[1], 3, 100, 0),
						genTx(t, signers[2], addresses[3], 1, 100, 0),
					},
					failed: []int{1, 2},
					state: map[types.Address]uint64{
						addresses[0]: 900,
						addresses[1]: 1000,
						addresses[3]: 100,
					},
				},
			},
		},
		{
			desc: "MultipleFromAccount",
			genesis: map[string]uint64{
				addresses[0].Hex(): 1000,
			},
			steps: []step{
				{
					txs: []*types.Transaction{
						genTx(t, signers[0], addresses[1], 1, 100, 0),
						genTx(t, signers[0], addresses[2], 2, 100, 0),
						genTx(t, signers[0], addresses[3], 3, 100, 0),
					},
					state: map[types.Address]uint64{
						addresses[0]: 700,
						addresses[1]: 100,
						addresses[2]: 100,
						addresses[3]: 100,
					},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			db := sql.InMemory()
			vm := New(logtest.New(t), db)
			require.NoError(t, vm.SetupGenesis(&config.GenesisConfig{Accounts: tc.genesis}))
			base := types.NewLayerID(1)
			for _, s := range tc.steps {
				if s.revert == 0 {
					failed, err := vm.ApplyLayer(base, s.txs, s.rewards)
					require.NoError(t, err)
					require.Len(t, failed, len(s.failed))
					for i, pos := range s.failed {
						require.Equal(t, s.txs[pos], failed[i])
					}
				} else {
					base = types.NewLayerID(s.revert)
					_, err := vm.Revert(base)
					require.NoError(t, err)
				}

				accounts, err := vm.GetAllAccounts()
				require.NoError(t, err)
				require.Len(t, accounts, len(s.state))
				for _, account := range accounts {
					require.Equal(t, int(s.state[account.Address]), int(account.Balance))
				}
				base = base.Add(1)
			}
		})
	}
}

func expectedHash(tb testing.TB, balances ...uint64) types.Hash32 {
	tb.Helper()
	hasher := sha256.New()
	buf := [8]byte{}
	for _, balance := range balances {
		binary.LittleEndian.PutUint64(buf[:], balance)
		hasher.Write(buf[:])
	}
	hash := types.Hash32{}
	hasher.Sum(hash[:0])
	return hash
}

func TestLayerHash(t *testing.T) {
	db := sql.InMemory()
	vm := New(logtest.New(t), db)

	rng := rand.New(rand.NewSource(101))
	signers := make([]*signing.EdSigner, 4)
	addresses := make([]types.Address, len(signers))
	for i := range signers {
		signer := signing.NewEdSignerFromRand(rng)
		signers[i] = signer
		addresses[i] = types.BytesToAddress(signer.PublicKey().Bytes())
	}

	rewards := []types.AnyReward{
		{
			Address: addresses[0], Amount: 100,
		},
		{
			Address: addresses[1], Amount: 1000,
		},
	}
	_, err := vm.ApplyLayer(types.NewLayerID(1), nil, rewards)
	require.NoError(t, err)
	root, err := vm.GetStateRoot()
	require.NoError(t, err)
	require.NotEqual(t, root, types.Hash32{})
	require.Equal(t, expectedHash(t, 100, 1000), root)

	txs := []*types.Transaction{
		genTx(t, signers[0], addresses[2], 1, 50, 0),
		genTx(t, signers[0], addresses[3], 2, 50, 0),
	}

	_, err = vm.ApplyLayer(types.NewLayerID(2), txs, rewards)
	require.NoError(t, err)
	root, err = vm.GetStateRoot()
	require.NoError(t, err)
	require.NotEqual(t, root, types.Hash32{})
	require.Equal(t, expectedHash(t, 100, 2000, 50, 50), root)
}
