package txs

import (
	"math"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func TestGetBlockTXs_OptimisticFiltering(t *testing.T) {
	accounts := createState(t, 100)
	num := 3
	mtxs := make([]*types.MeshTransaction, 0, num*len(accounts))
	expected := make([]types.TransactionID, 0, num*len(accounts))
	now := time.Now()
	for _, ta := range accounts {
		nextNonce := ta.nonce
		balance := ta.balance
		availPerTx := balance / uint64(num)
		fee := defaultFee * defaultGas
		if availPerTx <= fee {
			continue
		}
		amt := availPerTx - fee
		for i := 0; i < num; i++ {
			mtx := newMeshTX(t, nextNonce, ta.signer, amt, now)
			mtxs = append(mtxs, mtx)
			expected = append(expected, mtx.ID)
			nextNonce++
		}
	}
	blockSeed := types.RandomHash().Bytes()
	got, err := getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: true}, getStateFunc(accounts), blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Equal(t, len(expected), len(mtxs))
	require.ElementsMatch(t, expected, got)

	expSize := len(mtxs) / 2
	gasLimit := uint64(expSize) * defaultGas
	got, err = getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: true}, getStateFunc(accounts), blockSeed, gasLimit)
	require.NoError(t, err)
	require.Len(t, got, expSize)
}

func TestGetBlockTXs_OptimisticFiltering_SomeTXsApplied(t *testing.T) {
	accounts := createState(t, 100)
	num := 3
	mtxs := make([]*types.MeshTransaction, 0, num*len(accounts))
	expected := make([]types.TransactionID, 0, num*len(accounts))
	now := time.Now()
	for _, ta := range accounts {
		// causing the first tx for every account APPLIED
		nextNonce := ta.nonce - 1
		balance := ta.balance
		availPerTx := balance / uint64(num)
		fee := defaultFee * defaultGas
		if availPerTx <= fee {
			continue
		}
		amt := availPerTx - fee
		for i := 0; i < num; i++ {
			mtx := newMeshTX(t, nextNonce, ta.signer, amt, now)
			if i == 0 {
				mtx.State = types.APPLIED
			} else {
				expected = append(expected, mtx.ID)
			}
			mtxs = append(mtxs, mtx)
			nextNonce++
		}
	}
	blockSeed := types.RandomHash().Bytes()
	got, err := getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: true}, getStateFunc(accounts), blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Less(t, len(expected), len(mtxs))
	require.ElementsMatch(t, expected, got)
}

func TestGetBlockTXs_OptimisticFiltering_InsufficientBalance(t *testing.T) {
	accounts := createState(t, 100)
	num := 3
	mtxs := make([]*types.MeshTransaction, 0, num*len(accounts))
	expected := make([]types.TransactionID, 0, num*len(accounts))
	now := time.Now()
	for _, ta := range accounts {
		nextNonce := ta.nonce
		balance := ta.balance
		// cause the last transaction to fail the balance check
		availPerTx := balance / uint64(num-1)
		fee := defaultFee * defaultGas
		if availPerTx <= fee {
			continue
		}
		amt := availPerTx - fee
		for i := 0; i < num; i++ {
			mtx := newMeshTX(t, nextNonce, ta.signer, amt, now)
			mtxs = append(mtxs, mtx)
			if i < num-1 {
				expected = append(expected, mtx.ID)
			}
			nextNonce++
		}
	}
	blockSeed := types.RandomHash().Bytes()
	got, err := getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: true}, getStateFunc(accounts), blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Less(t, len(expected), len(mtxs))
	require.ElementsMatch(t, expected, got)
}

func TestGetBlockTXs_OptimisticFiltering_GapNonce(t *testing.T) {
	accounts := createState(t, 100)
	num := 3
	mtxs := make([]*types.MeshTransaction, 0, num*len(accounts))
	expected := make([]types.TransactionID, 0, num*len(accounts))
	now := time.Now()
	for _, ta := range accounts {
		nextNonce := ta.nonce
		balance := ta.balance
		availPerTx := balance / uint64(num)
		fee := defaultFee * defaultGas
		if availPerTx <= fee {
			continue
		}
		amt := availPerTx - fee
		for i := 0; i < num; i++ {
			// every tx has nonce gap
			nextNonce = nextNonce + 2
			mtx := newMeshTX(t, nextNonce, ta.signer, amt, now)
			mtxs = append(mtxs, mtx)
			expected = append(expected, mtx.ID)
		}
	}
	blockSeed := types.RandomHash().Bytes()
	got, err := getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: true}, getStateFunc(accounts), blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Equal(t, len(expected), len(mtxs))
	require.ElementsMatch(t, expected, got)
}

func TestGetBlockTXs_NoOptimisticFiltering(t *testing.T) {
	accounts := createState(t, 100)
	num := 3
	mtxs := make([]*types.MeshTransaction, 0, num*len(accounts))
	expected := make([]types.TransactionID, 0, num*len(accounts))
	now := time.Now()
	for _, ta := range accounts {
		nextNonce := ta.nonce
		balance := ta.balance
		// every tx used up the balance
		for i := 0; i < num; i++ {
			mtx := newMeshTX(t, nextNonce, ta.signer, balance, now)
			mtxs = append(mtxs, mtx)
			expected = append(expected, mtx.ID)
			nextNonce++
		}
	}
	blockSeed := types.RandomHash().Bytes()
	got, err := getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: false}, getStateFunc(accounts), blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Equal(t, len(expected), len(mtxs))
	require.ElementsMatch(t, expected, got)

	expSize := len(mtxs) / 2
	gasLimit := uint64(expSize) * defaultGas
	got, err = getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: false}, getStateFunc(accounts), blockSeed, gasLimit)
	require.NoError(t, err)
	require.Len(t, got, expSize)

	// no txs will return if optimistic filtering is ON
	got, err = getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: true}, getStateFunc(accounts), blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Empty(t, got)
}

func Test_checkStateConsensus(t *testing.T) {
	cfg := CSConfig{OptFilterThreshold: 100}
	lid := types.NewLayerID(11)
	meshHash := types.RandomHash()
	numProps := 10
	props := make([]*types.Proposal, 0, numProps)
	for i := 0; i < 10; i++ {
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				MeshHash: meshHash,
			},
		}
		props = append(props, p)
	}
	md, err := checkStateConsensus(logtest.New(t), cfg, lid, props, meshHash, nil)
	require.NoError(t, err)
	require.Equal(t, proposalMetadata{
		lid:  lid,
		size: numProps,
		meshHashes: map[types.Hash32]*meshState{
			meshHash: {hash: meshHash, count: numProps},
		},
		mtxs:      []*types.MeshTransaction{},
		optFilter: true,
	}, *md)
}

func Test_checkStateConsensus_allEmpty(t *testing.T) {
	cfg := CSConfig{OptFilterThreshold: 100}
	lid := types.NewLayerID(11)
	meshHash := types.EmptyLayerHash
	numProps := 10
	props := make([]*types.Proposal, 0, numProps)
	for i := 0; i < 10; i++ {
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				MeshHash: meshHash,
			},
		}
		props = append(props, p)
	}
	md, err := checkStateConsensus(logtest.New(t), cfg, lid, props, meshHash, nil)
	require.NoError(t, err)
	require.Equal(t, proposalMetadata{
		lid:  lid,
		size: numProps,
		meshHashes: map[types.Hash32]*meshState{
			meshHash: {hash: meshHash, count: numProps},
		},
		mtxs:      []*types.MeshTransaction{},
		optFilter: false,
	}, *md)
}

func Test_checkStateConsensus_ownMeshDiffer(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping test in Windows (https://github.com/spacemeshos/go-spacemesh/issues/3624)")
	}
	cfg := CSConfig{OptFilterThreshold: 100}
	lid := types.NewLayerID(11)
	meshHash := types.RandomHash()
	numProps := 10
	props := make([]*types.Proposal, 0, numProps)
	for i := 0; i < 10; i++ {
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				MeshHash: meshHash,
			},
		}
		props = append(props, p)
	}
	_, err := checkStateConsensus(logtest.New(t), cfg, lid, props, types.RandomHash(), nil)
	require.ErrorIs(t, err, errNodeHasBadMeshHash)
}

func Test_checkStateConsensus_NoConsensus(t *testing.T) {
	if os.Getenv("GOOS") == "windows" {
		t.Skip("Skipping test in Windows (https://github.com/spacemeshos/go-spacemesh/issues/3624)")
	}

	cfg := CSConfig{OptFilterThreshold: 100}
	lid := types.NewLayerID(11)
	meshHash0 := types.RandomHash()
	meshHash1 := types.RandomHash()
	numProps := 10
	props := make([]*types.Proposal, 0, numProps)
	for i := 0; i < 10; i++ {
		h := meshHash0
		if i%2 == 0 {
			h = meshHash1
		}
		p := &types.Proposal{
			InnerProposal: types.InnerProposal{
				MeshHash: h,
			},
		}
		props = append(props, p)
	}
	md, err := checkStateConsensus(logtest.New(t), cfg, lid, props, meshHash1, nil)
	require.NoError(t, err)
	require.Equal(t, proposalMetadata{
		lid:  lid,
		size: numProps,
		meshHashes: map[types.Hash32]*meshState{
			meshHash0: {hash: meshHash0, count: numProps / 2},
			meshHash1: {hash: meshHash1, count: numProps / 2},
		},
		mtxs:      []*types.MeshTransaction{},
		optFilter: false,
	}, *md)
}
