package vm

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/vm/transaction"
)

func genTx(tb testing.TB, signer *signing.EdSigner, receiver types.Address, nonce, amount, fee uint64) *types.Transaction {
	tb.Helper()
	tx, err := transaction.GenerateCallTransaction(signer, receiver, nonce, amount, 0, fee)
	require.NoError(tb, err)
	return tx
}

type step struct {
	txs          []*types.Transaction
	failed       []int
	rewards      []types.AnyReward
	finalRewards []*types.Reward

	revert uint32 // number of the layer
	state  map[types.Address]uint64
}

func TestLayers(t *testing.T) {
	types.SetLayersPerEpoch(4)
	rng := rand.New(rand.NewSource(101))
	signers := make([]*signing.EdSigner, 10)
	addresses := make([]types.Address, len(signers))
	weights := make([]util.Weight, len(signers))
	weightBytes := make([][]byte, len(signers))
	for i := range signers {
		signer := signing.NewEdSignerFromRand(rng)
		signers[i] = signer
		addresses[i] = types.BytesToAddress(signer.PublicKey().Bytes())
		weight := util.WeightFromFloat64(13.37).Add(util.WeightFromInt64(int64(i)))
		weights[i] = weight
		wb, err := weight.GobEncode()
		require.NoError(t, err)
		weightBytes[i] = wb
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
					// no transactions are applied
					rewards: []types.AnyReward{
						{Coinbase: addresses[0], Weight: weightBytes[0]},
						{Coinbase: addresses[1], Weight: weightBytes[1]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[0], TotalReward: 24098774333093, LayerReward: 24098774333093},
						{Coinbase: addresses[1], TotalReward: 25901225666906, LayerReward: 25901225666906},
					},
					state: map[types.Address]uint64{
						addresses[0]: 24098774333093,
						addresses[1]: 25901225666906,
					},
				},
				{
					txs: []*types.Transaction{
						genTx(t, signers[0], addresses[2], 0, 50, 10),
						genTx(t, signers[1], addresses[2], 0, 50, 10),
					},
					rewards: []types.AnyReward{
						{Coinbase: addresses[1], Weight: weightBytes[1]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[1], TotalReward: 50000000000020, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 24098774333033,
						addresses[1]: 75901225666866,
						addresses[2]: 100,
					},
				},
			},
		},
		{
			desc: "RewardsSameCoinbase",
			steps: []step{
				{
					// no transactions are applied
					rewards: []types.AnyReward{
						{Coinbase: addresses[0], Weight: weightBytes[0]},
						{Coinbase: addresses[0], Weight: weightBytes[0]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[0], TotalReward: 50000000000000, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 50000000000000,
					},
				},
				{
					// no transactions are applied
					rewards: []types.AnyReward{
						{Coinbase: addresses[0], Weight: weightBytes[0]},
						{Coinbase: addresses[0], Weight: weightBytes[0]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[0], TotalReward: 50000000000000, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 100000000000000,
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
						genTx(t, signers[1], addresses[0], 0, 90, 10),
					},
					rewards: []types.AnyReward{
						{Coinbase: addresses[2], Weight: weightBytes[2]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[2], TotalReward: 50000000000010, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 190,
						addresses[1]: 0,
						addresses[2]: 50000000000010,
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
						genTx(t, signers[1], addresses[2], 0, 80, 10),
					},
					rewards: []types.AnyReward{
						{Coinbase: addresses[2], Weight: weightBytes[2]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[2], TotalReward: 50000000000010, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 10,
						addresses[2]: 50000000000090,
					},
				},
				{
					rewards: []types.AnyReward{
						{Coinbase: addresses[3], Weight: weightBytes[3]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[3], TotalReward: 50000000000000, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 10,
						addresses[2]: 50000000000090,
						addresses[3]: 50000000000000,
					},
				},
				{
					revert: 1,
					state: map[types.Address]uint64{
						addresses[0]: 100,
						addresses[1]: 10,
						addresses[2]: 50000000000090,
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
						genTx(t, signers[0], addresses[3], 0, 100, 100),
						genTx(t, signers[0], addresses[1], 3, 100, 100),
						genTx(t, signers[2], addresses[3], 0, 100, 100),
					},
					rewards: []types.AnyReward{
						{Coinbase: addresses[2], Weight: weightBytes[2]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[2], TotalReward: 50000000000100, LayerReward: 50000000000000},
					},
					failed: []int{1, 2},
					state: map[types.Address]uint64{
						addresses[0]: 800,
						addresses[1]: 1000,
						addresses[2]: 50000000000100,
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
						genTx(t, signers[0], addresses[1], 0, 100, 1),
						genTx(t, signers[0], addresses[2], 1, 100, 1),
						genTx(t, signers[0], addresses[3], 2, 100, 1),
					},
					rewards: []types.AnyReward{
						{Coinbase: addresses[2], Weight: weightBytes[2]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[2], TotalReward: 50000000000003, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 697,
						addresses[1]: 100,
						addresses[2]: 50000000000103,
						addresses[3]: 100,
					},
				},
			},
		},
		{
			desc: "OutOfOrder",
			genesis: map[string]uint64{
				addresses[0].Hex(): 1000,
			},
			steps: []step{
				{
					txs: []*types.Transaction{
						genTx(t, signers[0], addresses[1], 2, 100, 1),
						genTx(t, signers[0], addresses[2], 0, 100, 1),
						genTx(t, signers[0], addresses[3], 1, 100, 1),
					},
					rewards: []types.AnyReward{
						{Coinbase: addresses[2], Weight: weightBytes[2]},
					},
					finalRewards: []*types.Reward{
						{Coinbase: addresses[2], TotalReward: 50000000000003, LayerReward: 50000000000000},
					},
					state: map[types.Address]uint64{
						addresses[0]: 697,
						addresses[1]: 100,
						addresses[2]: 50000000000103,
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
			base := types.GetEffectiveGenesis()
			for _, s := range tc.steps {
				base = base.Add(1)

				if s.revert > 0 {
					base = types.GetEffectiveGenesis().Add(s.revert)
					_, err := vm.Revert(base)
					require.NoError(t, err)
				} else if len(s.rewards) > 0 {
					failed, rewards, err := vm.ApplyLayer(base, s.txs, s.rewards)
					require.NoError(t, err)
					require.Len(t, failed, len(s.failed))
					for i, pos := range s.failed {
						require.Equal(t, s.txs[pos], failed[i])
					}
					var totalLayerRewards uint64
					for i, r := range rewards {
						require.Equal(t, base, r.Layer)
						require.Equal(t, s.finalRewards[i].Coinbase, r.Coinbase)
						require.Equal(t, s.finalRewards[i].TotalReward, r.TotalReward)
						require.Equal(t, s.finalRewards[i].LayerReward, r.LayerReward)
						totalLayerRewards += r.LayerReward
					}
					require.LessOrEqual(t, totalLayerRewards, calculateLayerReward(vm.cfg))
				}

				accounts, err := vm.GetAllAccounts()
				require.NoError(t, err)
				require.Len(t, accounts, len(s.state))
				for _, account := range accounts {
					require.Equal(t, int(s.state[account.Address]), int(account.Balance))
				}
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
	weights := make([]util.Weight, len(signers))
	weightBytes := make([][]byte, len(signers))
	for i := range signers {
		signer := signing.NewEdSignerFromRand(rng)
		signers[i] = signer
		addresses[i] = types.BytesToAddress(signer.PublicKey().Bytes())
		weight := util.WeightFromFloat64(13.37).Add(util.WeightFromInt64(int64(i)))
		weights[i] = weight
		wb, err := weight.GobEncode()
		require.NoError(t, err)
		weightBytes[i] = wb
	}

	rewards := []types.AnyReward{
		{Coinbase: addresses[0], Weight: weightBytes[0]},
		{Coinbase: addresses[1], Weight: weightBytes[1]},
	}

	_, _, err := vm.ApplyLayer(types.NewLayerID(1), nil, rewards)
	require.NoError(t, err)
	root, err := vm.GetStateRoot()
	require.NoError(t, err)
	require.NotEqual(t, root, types.Hash32{})
	require.Equal(t, expectedHash(t, 24098774333093, 25901225666906), root)

	txs := []*types.Transaction{
		genTx(t, signers[0], addresses[2], 0, 50, 0),
		genTx(t, signers[0], addresses[3], 1, 50, 0),
	}

	_, _, err = vm.ApplyLayer(types.NewLayerID(2), txs, rewards)
	require.NoError(t, err)
	root, err = vm.GetStateRoot()
	require.NoError(t, err)
	require.NotEqual(t, root, types.Hash32{})
	require.Equal(t, expectedHash(t, 48197548666086, 50, 50, 51802451333812), root)
}
