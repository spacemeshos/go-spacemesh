package txs

import (
	"math"
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
		if availPerTx <= defaultFee {
			continue
		}
		amt := availPerTx - defaultFee
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
		if availPerTx <= defaultFee {
			continue
		}
		amt := availPerTx - defaultFee
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
		if availPerTx <= defaultFee {
			continue
		}
		amt := availPerTx - defaultFee
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

func TestGetBlockTXs_OptimisticFiltering_BadNonce(t *testing.T) {
	accounts := createState(t, 100)
	num := 3
	mtxs := make([]*types.MeshTransaction, 0, num*len(accounts))
	expected := make([]types.TransactionID, 0, num*len(accounts))
	now := time.Now()
	for _, ta := range accounts {
		nextNonce := ta.nonce
		balance := ta.balance
		availPerTx := balance / uint64(num)
		if availPerTx <= defaultFee {
			continue
		}
		amt := availPerTx - defaultFee
		for i := 0; i < num; i++ {
			mtx := newMeshTX(t, nextNonce, ta.signer, amt, now)
			mtxs = append(mtxs, mtx)
			// cause the following transaction to fail the nonce check
			nextNonce = nextNonce + 2
			if i == 0 {
				expected = append(expected, mtx.ID)
			}
		}
	}
	blockSeed := types.RandomHash().Bytes()
	got, err := getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs, optFilter: true}, getStateFunc(accounts), blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Less(t, len(expected), len(mtxs))
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
