package txs

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

func Test_getBlockTXs(t *testing.T) {
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
			nextNonce = nextNonce + 2
		}
	}
	blockSeed := types.RandomHash().Bytes()
	got, err := getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs}, blockSeed, 0)
	require.NoError(t, err)
	require.Equal(t, len(expected), len(mtxs))
	require.ElementsMatch(t, expected, got)

	got, err = getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs}, blockSeed, math.MaxUint64)
	require.NoError(t, err)
	require.Equal(t, len(expected), len(mtxs))
	require.ElementsMatch(t, expected, got)

	expSize := len(mtxs) / 2
	gasLimit := uint64(expSize) * defaultGas
	got, err = getBlockTXs(logtest.New(t), &proposalMetadata{mtxs: mtxs}, blockSeed, gasLimit)
	require.NoError(t, err)
	require.Len(t, got, expSize)
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
	if util.IsWindows() {
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
	if util.IsWindows() {
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
