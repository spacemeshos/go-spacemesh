package blocks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/blocks/mocks"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type testHandler struct {
	*Handler
	mockFetcher  *smocks.MockFetcher
	mockTortoise *mocks.MocktortoiseProvider
	mockMesh     *mocks.MockmeshProvider
}

func createTestHandler(t *testing.T) *testHandler {
	ctrl := gomock.NewController(t)
	th := &testHandler{
		mockFetcher:  smocks.NewMockFetcher(ctrl),
		mockTortoise: mocks.NewMocktortoiseProvider(ctrl),
		mockMesh:     mocks.NewMockmeshProvider(ctrl),
	}
	th.Handler = NewHandler(
		th.mockFetcher,
		statesql.InMemory(),
		th.mockTortoise,
		th.mockMesh,
		WithLogger(zaptest.NewLogger(t)),
	)
	return th
}

func TestHandleSyncedBlock(t *testing.T) {
	layer := types.GetEffectiveGenesis() + 1
	good := &types.Block{InnerBlock: types.InnerBlock{
		LayerIndex: layer,
		Rewards: []types.AnyReward{
			{AtxID: types.ATXID{1}, Weight: types.RatNum{Num: 1, Denom: 1}},
			{AtxID: types.ATXID{2}, Weight: types.RatNum{Num: 1, Denom: 1}},
		},
		TxIDs: []types.TransactionID{{1}, {2}},
	}}
	good.Initialize()

	badrewards := &types.Block{InnerBlock: types.InnerBlock{
		LayerIndex: layer,
		Rewards: []types.AnyReward{
			{AtxID: types.ATXID{1}},
			{AtxID: types.ATXID{2}},
		},
	}}
	badrewards.Initialize()

	beforegenesis := &types.Block{InnerBlock: types.InnerBlock{
		LayerIndex: types.GetEffectiveGenesis(),
	}}
	beforegenesis.Initialize()
	for _, tc := range []struct {
		desc              string
		data              []byte
		id                types.Hash32
		dup               bool
		epoch             types.EpochID
		tortoise, missing []types.ATXID
		failAtxs          error
		txs               []types.TransactionID
		failTxs           error
		failMesh          error
		err               string
	}{
		{
			desc:     "sanity",
			data:     codec.MustEncode(good),
			id:       good.ID().AsHash32(),
			epoch:    good.LayerIndex.GetEpoch(),
			tortoise: []types.ATXID{{1}, {2}},
			missing:  []types.ATXID{{2}},
			txs:      []types.TransactionID{{1}, {2}},
		},
		{
			desc: "malformed",
			data: []byte("any"),
			err:  "validation reject: malformed",
		},
		{
			desc: "hash mismatch",
			data: codec.MustEncode(good),
			id:   types.Hash32{1, 1, 1},
			err:  "validation reject: incorrect hash",
		},
		{
			desc: "invalid rewards",
			data: codec.MustEncode(badrewards),
			id:   badrewards.ID().AsHash32(),
			err:  "validation reject: reward with invalid",
		},
		{
			desc: "duplicate",
			data: codec.MustEncode(good),
			id:   good.ID().AsHash32(),
			dup:  true,
		},
		{
			desc: "layer before genesis",
			data: codec.MustEncode(beforegenesis),
			id:   beforegenesis.ID().AsHash32(),
			err:  "validation reject: block before effective genesis",
		},
		{
			desc:     "fetch atxs failure",
			data:     codec.MustEncode(good),
			id:       good.ID().AsHash32(),
			epoch:    good.LayerIndex.GetEpoch(),
			tortoise: []types.ATXID{{1}, {2}},
			missing:  []types.ATXID{{2}},
			failAtxs: errors.New("atxs failed"),
			err:      "atxs failed",
		},
		{
			desc:     "fetch txs failure",
			data:     codec.MustEncode(good),
			id:       good.ID().AsHash32(),
			epoch:    good.LayerIndex.GetEpoch(),
			tortoise: []types.ATXID{{1}, {2}},
			missing:  []types.ATXID{{2}},
			txs:      []types.TransactionID{{1}, {2}},
			failTxs:  errors.New("txs failed"),
			err:      "txs failed",
		},
		{
			desc:     "add block failure",
			data:     codec.MustEncode(good),
			id:       good.ID().AsHash32(),
			epoch:    good.LayerIndex.GetEpoch(),
			tortoise: []types.ATXID{{1}, {2}},
			missing:  []types.ATXID{{2}},
			txs:      []types.TransactionID{{1}, {2}},
			failMesh: errors.New("add block failed"),
			err:      "add block failed",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			th := createTestHandler(t)
			pid := p2p.Peer("test")
			var decoded types.Block
			codec.Decode(tc.data, &decoded)
			decoded.Initialize()
			if tc.dup {
				require.NoError(t, blocks.Add(th.db, &decoded))
			}

			th.mockTortoise.EXPECT().GetMissingActiveSet(tc.epoch, tc.tortoise).Return(tc.missing).MaxTimes(1)
			th.mockFetcher.EXPECT().RegisterPeerHashes(pid, types.ATXIDsToHashes(tc.missing)).MaxTimes(1)
			th.mockFetcher.EXPECT().GetAtxs(gomock.Any(), tc.missing).Return(tc.failAtxs).MaxTimes(1)
			th.mockFetcher.EXPECT().RegisterPeerHashes(pid, types.TransactionIDsToHashes(tc.txs)).MaxTimes(1)
			th.mockFetcher.EXPECT().GetBlockTxs(gomock.Any(), tc.txs).Return(tc.failTxs).MaxTimes(1)
			th.mockMesh.EXPECT().AddBlockWithTXs(gomock.Any(), &decoded).Return(tc.failMesh).MaxTimes(1)

			err := th.HandleSyncedBlock(context.Background(), tc.id, pid, tc.data)
			if len(tc.err) > 0 {
				require.ErrorContains(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateRewards(t *testing.T) {
	for _, tc := range []struct {
		desc    string
		rewards []types.AnyReward
		err     bool
	}{
		{
			desc: "sanity",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{2},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
			},
		},
		{
			desc:    "empty",
			rewards: []types.AnyReward{},
			err:     true,
		},
		{
			desc: "nil",
			err:  true,
		},
		{
			desc: "zero num",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 0, Denom: 3},
				},
			},
			err: true,
		},
		{
			desc: "zero denom",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 1, Denom: 0},
				},
			},
			err: true,
		},
		{
			desc: "multiple per coinbase",
			rewards: []types.AnyReward{
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{3},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
				{
					AtxID:  types.ATXID{1},
					Weight: types.RatNum{Num: 1, Denom: 3},
				},
			},
			err: true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateRewards(tc.rewards)
			if tc.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
