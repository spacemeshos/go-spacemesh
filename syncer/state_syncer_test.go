package syncer

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func opinions() []*fetch.LayerOpinion {
	return []*fetch.LayerOpinion{
		{
			EpochWeight: 100,
			Verified:    types.NewLayerID(17),
			Cert: &types.Certificate{
				BlockID: types.RandomBlockID(),
			},
			Valid: []types.BlockID{types.RandomBlockID()},
		},
		{
			EpochWeight: 120,
			Verified:    types.NewLayerID(16),
			Cert: &types.Certificate{
				BlockID: types.RandomBlockID(),
			},
			Valid: []types.BlockID{types.RandomBlockID()},
		},
		{
			EpochWeight: 100,
			Verified:    types.NewLayerID(18),
			Valid:       []types.BlockID{types.RandomBlockID()},
		},
		{
			EpochWeight: 100,
			Verified:    types.NewLayerID(18),
			Cert: &types.Certificate{
				BlockID: types.RandomBlockID(),
			},
			Valid: []types.BlockID{types.RandomBlockID()},
		},
	}
}

func okOpnCh(lid types.LayerID, opinions []*fetch.LayerOpinion) chan fetch.LayerPromiseOpinions {
	ch := make(chan fetch.LayerPromiseOpinions, 1)
	ch <- fetch.LayerPromiseOpinions{
		Layer:    lid,
		Opinions: opinions,
	}
	close(ch)
	return ch
}

func failedOpnCh() chan fetch.LayerPromiseOpinions {
	ch := make(chan fetch.LayerPromiseOpinions, 1)
	ch <- fetch.LayerPromiseOpinions{
		Err: errors.New("something baaahhhhhhd"),
	}
	close(ch)
	return ch
}

func checkHasBlockValidity(t *testing.T, db sql.Executor, lid types.LayerID) {
	cnt, err := blocks.CountContextualValidity(db, lid)
	require.NoError(t, err)
	require.Greater(t, cnt, 0)
}

func TestProcessLayers_AllGood(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(10)
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	ts.mTicker.advanceToLayer(current)
	layerOpns := make(map[types.LayerID][]*fetch.LayerOpinion)
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		lid := lid
		layerOpns[lid] = opinions()
		cert := layerOpns[lid][1].Cert
		ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
		ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
		ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(okOpnCh(lid, layerOpns[lid]))
		ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
		ts.mCertHdr.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, cert).DoAndReturn(
			func(_ context.Context, gotL types.LayerID, gotC *types.Certificate) error {
				require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(gotC.BlockID, types.InnerBlock{LayerIndex: lid})))
				require.NoError(t, layers.SetHareOutputWithCert(ts.cdb, gotL, gotC))
				return nil
			})
		ts.mLyrFetcher.EXPECT().GetBlocks(gomock.Any(), layerOpns[lid][1].Valid).DoAndReturn(
			func(_ context.Context, bids []types.BlockID) error {
				for _, bid := range bids {
					require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: lid})))
				}
				return nil
			})
		ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(lid.Sub(1))
		ts.mConState.EXPECT().ApplyLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got *types.Block) error {
				require.Equal(t, cert.BlockID, got.ID())
				return nil
			})
		ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	}
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		checkHasBlockValidity(t, ts.cdb, lid)
	}
}

func TestProcessLayers_DespiteMissingCert(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(10)
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	ts.mTicker.advanceToLayer(current)
	layerOpns := make(map[types.LayerID][]*fetch.LayerOpinion)
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		lid := lid
		layerOpns[lid] = []*fetch.LayerOpinion{
			{
				EpochWeight: 100,
				Verified:    types.NewLayerID(19),
				Valid:       []types.BlockID{types.RandomBlockID()},
			},
		}
		ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
		ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
		ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(okOpnCh(lid, layerOpns[lid]))
		ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
		ts.mLyrFetcher.EXPECT().GetBlocks(gomock.Any(), layerOpns[lid][0].Valid).DoAndReturn(
			func(_ context.Context, bids []types.BlockID) error {
				for _, bid := range bids {
					require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: lid})))
				}
				return nil
			})
		ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(glayer)
	}
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		checkHasBlockValidity(t, ts.cdb, lid)
	}
}

func TestProcessLayers_OpinionsNotNeeded(t *testing.T) {
	gLid := types.GetEffectiveGenesis()
	tt := []struct {
		name                             string
		requested, verified, opnVerified types.LayerID
		hasOpns                          bool
	}{
		{
			name:        "node already has opinions",
			hasOpns:     true,
			requested:   gLid.Add(1),
			verified:    gLid,
			opnVerified: gLid.Add(10),
		},
		{
			name:        "no higher verified from peers",
			requested:   gLid.Add(1),
			verified:    gLid,
			opnVerified: gLid,
		},
		{
			name:        "node already verified",
			requested:   gLid.Add(1),
			verified:    gLid.Add(1),
			opnVerified: gLid.Add(10),
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := newSyncerWithoutSyncTimer(t)
			ts.syncer.setATXSynced()
			current := tc.requested.Add(1)
			ts.syncer.setLastSyncedLayer(current.Sub(1))
			ts.mTicker.advanceToLayer(current)

			// saves opinions
			if tc.hasOpns {
				bid := types.RandomBlockID()
				require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: tc.requested})))
				require.NoError(t, layers.SetHareOutputWithCert(ts.cdb, tc.requested, &types.Certificate{BlockID: bid}))
				require.NoError(t, blocks.SetValid(ts.cdb, bid))
				ts.mConState.EXPECT().ApplyLayer(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got *types.Block) error {
						require.Equal(t, bid, got.ID())
						return nil
					})
				ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
			}
			ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
			ts.mLyrPatrol.EXPECT().IsHareInCharge(tc.requested).Return(false)
			opns := opinions()
			for _, opn := range opns {
				opn.Verified = tc.opnVerified
			}
			ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), tc.requested).Return(okOpnCh(tc.requested, opns))
			ts.mTortoise.EXPECT().LatestComplete().Return(tc.verified)
			ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), tc.requested).Return(tc.requested.Sub(1))

			require.False(t, ts.syncer.stateSynced())
			require.NoError(t, ts.syncer.processLayers(context.TODO()))
			require.True(t, ts.syncer.stateSynced())
		})
	}
}

func TestProcessLayers_BeaconNotAvailable(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.EmptyBeacon, errBeaconNotAvailable)
	require.False(t, ts.syncer.stateSynced())
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errBeaconNotAvailable)
	require.False(t, ts.syncer.stateSynced())
}

func TestProcessLayers_ATXsNotSynced(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	glayer := types.GetEffectiveGenesis()
	current := glayer.Add(10)
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	ts.mTicker.advanceToLayer(current)
	require.False(t, ts.syncer.stateSynced())
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errATXsNotSynced)
	require.False(t, ts.syncer.stateSynced())
}

func TestProcessLayers_Shutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	ts := newTestSyncer(ctx, t, never)
	ts.syncer.setATXSynced()

	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))

	cancel()
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errShuttingDown)
	require.False(t, ts.syncer.stateSynced())
}

func TestProcessLayers_HareIsStillWorking(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))

	require.False(t, ts.syncer.stateSynced())
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
	ts.mLyrPatrol.EXPECT().IsHareInCharge(lastSynced).Return(true)
	require.ErrorIs(t, ts.syncer.processLayers(context.TODO()), errHareInCharge)
	require.False(t, ts.syncer.stateSynced())

	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
	ts.mLyrPatrol.EXPECT().IsHareInCharge(lastSynced).Return(false)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lastSynced).Return(okOpnCh(lastSynced, nil))
	ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lastSynced).Return(lastSynced)
	ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_HareTakesTooLong(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	glayer := types.GetEffectiveGenesis()
	lastSynced := glayer.Add(ts.syncer.conf.HareDelayLayers)
	ts.syncer.setLastSyncedLayer(lastSynced)
	current := lastSynced.Add(1)
	ts.mTicker.advanceToLayer(current)
	for lid := glayer.Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
		if lid == glayer.Add(1) {
			ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(true)
		} else {
			ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
		}
		ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(okOpnCh(lid, nil))
		ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lid).Return(glayer)
	}
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_OpinionsOptional(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	lastSynced := types.GetEffectiveGenesis().Add(1)
	ts.syncer.setLastSyncedLayer(lastSynced)
	ts.mTicker.advanceToLayer(lastSynced.Add(1))
	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
	ts.mLyrPatrol.EXPECT().IsHareInCharge(lastSynced).Return(false)
	ts.mLyrFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lastSynced).Return(failedOpnCh())
	ts.mTortoise.EXPECT().HandleIncomingLayer(gomock.Any(), lastSynced).Return(types.GetEffectiveGenesis())
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestSortOpinions(t *testing.T) {
	sorted := opinions()
	unsorted := make([]*fetch.LayerOpinion, len(sorted))
	copy(unsorted, sorted)
	sortOpinions(sorted)
	require.Equal(t, unsorted[1], sorted[0])
	require.Equal(t, unsorted[3], sorted[1])
	require.Equal(t, unsorted[2], sorted[2])
	require.Equal(t, unsorted[0], sorted[3])
}
