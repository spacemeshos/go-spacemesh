package syncer

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func opinions(prevHash types.Hash32) []*fetch.LayerOpinion {
	return []*fetch.LayerOpinion{
		{
			EpochWeight: 100,
			Verified:    types.NewLayerID(17),
			PrevAggHash: prevHash,
			Cert: &types.Certificate{
				BlockID: types.RandomBlockID(),
			},
			Valid:   []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
			Invalid: []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
		},
		{
			EpochWeight: 120,
			Verified:    types.NewLayerID(16),
			PrevAggHash: prevHash,
			Cert: &types.Certificate{
				BlockID: types.RandomBlockID(),
			},
			Valid:   []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
			Invalid: []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
		},
		{
			EpochWeight: 100,
			Verified:    types.NewLayerID(18),
			PrevAggHash: prevHash,
			Valid:       []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
			Invalid:     []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
		},
		{
			EpochWeight: 100,
			Verified:    types.NewLayerID(18),
			PrevAggHash: prevHash,
			Cert: &types.Certificate{
				BlockID: types.RandomBlockID(),
			},
			Valid:   []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
			Invalid: []types.BlockID{types.RandomBlockID(), types.RandomBlockID()},
		},
	}
}

func checkHasBlockValidity(t *testing.T, db sql.Executor, lid types.LayerID) {
	cnt, err := blocks.CountContextualValidity(db, lid)
	require.NoError(t, err)
	require.Greater(t, cnt, 0)
}

func TestProcessLayers_MultiLayers(t *testing.T) {
	gLid := types.GetEffectiveGenesis()
	tt := []struct {
		name                             string
		requested, verified, opnVerified types.LayerID
		hasCert                          bool
	}{
		{
			name:    "all good",
			hasCert: true,
		},
		{
			name: "cert missing",
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := newSyncerWithoutSyncTimer(t)
			ts.syncer.setATXSynced()
			current := gLid.Add(10)
			ts.syncer.setLastSyncedLayer(current.Sub(1))
			ts.mTicker.advanceToLayer(current)
			layerOpns := make(map[types.LayerID][]*fetch.LayerOpinion)
			adopted := make(map[types.LayerID]*fetch.LayerOpinion)
			for lid := gLid.Add(1); lid.Before(current); lid = lid.Add(1) {
				prevHash := types.RandomHash()
				if lid.Sub(1) == gLid {
					h, err := layers.GetAggregatedHash(ts.cdb, lid.Sub(1))
					require.NoError(t, err)
					prevHash = h
				}
				opns := opinions(prevHash)
				if !tc.hasCert {
					for _, opn := range opns {
						opn.Cert = nil
					}
				}
				layerOpns[lid] = opns
				adopted[lid] = opns[1]
			}
			for lid := gLid.Add(1); lid.Before(current); lid = lid.Add(1) {
				lid := lid
				cert := layerOpns[lid][1].Cert
				ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
				ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
				ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).DoAndReturn(
					func(_ context.Context, lid types.LayerID) ([]*fetch.LayerOpinion, error) {
						// fake the prev agg hash from opinions
						prevLid := lid.Sub(1)
						if prevLid.After(gLid) {
							require.NoError(t, layers.SetHashes(ts.cdb, prevLid, types.RandomHash(), adopted[lid].PrevAggHash))
						}
						return layerOpns[lid], nil
					})
				ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
				if tc.hasCert {
					ts.mCertHdr.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, cert).DoAndReturn(
						func(_ context.Context, gotL types.LayerID, gotC *types.Certificate) error {
							require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(gotC.BlockID, types.InnerBlock{LayerIndex: lid})))
							require.NoError(t, layers.SetHareOutputWithCert(ts.cdb, gotL, gotC))
							return nil
						})
				}
				ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got []types.BlockID) error {
						require.ElementsMatch(t, append(adopted[lid].Valid, adopted[lid].Invalid...), got)
						for _, bid := range got {
							require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: lid})))
						}
						return nil
					})
				ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
				ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
				if tc.hasCert || lid != current.Sub(1) {
					ts.mConState.EXPECT().ApplyLayer(gomock.Any(), gomock.Any()).DoAndReturn(
						func(_ context.Context, got *types.Block) error {
							if tc.hasCert {
								require.Equal(t, cert.BlockID, got.ID())
							} else {
								types.SortBlockIDs(adopted[lid].Valid)
								require.Equal(t, adopted[lid].Valid[0], got.ID())
							}
							return nil
						})
					ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
				}
			}
			require.False(t, ts.syncer.stateSynced())
			require.NoError(t, ts.syncer.processLayers(context.TODO()))
			require.True(t, ts.syncer.stateSynced())
			for lid := gLid.Add(1); lid.Before(current); lid = lid.Add(1) {
				checkHasBlockValidity(t, ts.cdb, lid)
			}
		})
	}
}

func TestProcessLayers_OpinionsNotAdopted(t *testing.T) {
	gLid := types.GetEffectiveGenesis()
	tt := []struct {
		name                             string
		requested, verified, opnVerified types.LayerID
		localOpn                         types.BlockID
	}{
		{
			name:        "node already has opinions",
			localOpn:    types.RandomBlockID(),
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
		{
			name:        "different prev hash",
			requested:   gLid.Add(1),
			verified:    gLid,
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
			if tc.localOpn != types.EmptyBlockID {
				require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(tc.localOpn, types.InnerBlock{LayerIndex: tc.requested})))
				require.NoError(t, layers.SetHareOutputWithCert(ts.cdb, tc.requested, &types.Certificate{BlockID: tc.localOpn}))
				require.NoError(t, blocks.SetValid(ts.cdb, tc.localOpn))
				ts.mConState.EXPECT().ApplyLayer(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got *types.Block) error {
						require.Equal(t, tc.localOpn, got.ID())
						return nil
					})
				ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
			}
			ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
			ts.mLyrPatrol.EXPECT().IsHareInCharge(tc.requested).Return(false)
			opns := opinions(types.Hash32{})
			for _, opn := range opns {
				opn.Verified = tc.opnVerified
				opn.Cert = nil
			}
			ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), tc.requested).Return(opns, nil)
			ts.mTortoise.EXPECT().LatestComplete().Return(tc.verified)
			ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), tc.requested)
			ts.mTortoise.EXPECT().LatestComplete().Return(tc.requested.Sub(1))

			require.False(t, ts.syncer.stateSynced())
			require.NoError(t, ts.syncer.processLayers(context.TODO()))
			require.True(t, ts.syncer.stateSynced())
			if tc.localOpn == types.EmptyBlockID {
				need, err := ts.syncer.needCert(logtest.New(t), tc.requested)
				require.NoError(t, err)
				require.True(t, need)
				need, err = ts.syncer.needValidity(logtest.New(t), tc.requested)
				require.NoError(t, err)
				require.True(t, need)
			} else {
				bv, err := blocks.ContextualValidity(ts.cdb, tc.requested)
				require.NoError(t, err)
				require.Len(t, bv, 1)
				require.Equal(t, tc.localOpn, bv[0].ID)
				require.True(t, bv[0].Validity)
			}
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
	ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lastSynced).Return(nil, nil)
	ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lastSynced)
	ts.mTortoise.EXPECT().LatestComplete().Return(lastSynced)
	ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_HareTakesTooLong(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	glayer := types.GetEffectiveGenesis()
	lastSynced := glayer.Add(ts.syncer.cfg.HareDelayLayers)
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
		ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(nil, nil)
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().LatestComplete().Return(glayer)
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
	ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lastSynced).Return(nil, errors.New("meh"))
	ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lastSynced)
	ts.mTortoise.EXPECT().LatestComplete().Return(types.GetEffectiveGenesis())
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestSortOpinions(t *testing.T) {
	sorted := opinions(types.Hash32{})
	unsorted := make([]*fetch.LayerOpinion, len(sorted))
	copy(unsorted, sorted)
	sortOpinions(sorted)
	require.Equal(t, unsorted[1], sorted[0])
	require.Equal(t, unsorted[3], sorted[1])
	require.Equal(t, unsorted[2], sorted[2])
	require.Equal(t, unsorted[0], sorted[3])
}

func TestAdopt(t *testing.T) {
	gLid := types.GetEffectiveGenesis()
	prevHash := types.RandomHash()
	tt := []struct {
		name                   string
		opns                   []*fetch.LayerOpinion
		exp                    int
		err, certErr, fetchErr error
	}{
		{
			name: "all good",
			exp:  1,
			opns: opinions(prevHash),
		},
		{
			name: "no better opinions",
			err:  errNoBetterOpinions,
			opns: []*fetch.LayerOpinion{
				{Verified: gLid},
				{Verified: gLid},
				{Verified: gLid},
			},
		},
		{
			name: "cert missing from all",
			err:  errMissingCertificate,
			opns: []*fetch.LayerOpinion{
				{Verified: types.NewLayerID(11)},
				{Verified: types.NewLayerID(11)},
				{Verified: types.NewLayerID(11)},
			},
		},
		{
			name: "cert not adopted",
			opns: []*fetch.LayerOpinion{
				{Verified: types.NewLayerID(11), Cert: &types.Certificate{BlockID: types.RandomBlockID()}},
				{Verified: types.NewLayerID(11)},
				{Verified: types.NewLayerID(11)},
			},
			certErr: errors.New("meh"),
			err:     errNoCertAdopted,
		},
		{
			name: "validity missing from all",
			opns: []*fetch.LayerOpinion{
				{Verified: types.NewLayerID(11), Cert: &types.Certificate{BlockID: types.RandomBlockID()}},
				{Verified: types.NewLayerID(11), Cert: &types.Certificate{BlockID: types.RandomBlockID()}},
				{Verified: types.NewLayerID(11), Cert: &types.Certificate{BlockID: types.RandomBlockID()}},
			},
			err: errMissingValidity,
		},
		{
			name:     "blocks missing",
			exp:      1,
			opns:     opinions(prevHash),
			fetchErr: errors.New("meh"),
			err:      errNoValidityAdopted,
		},
		{
			name: "no matching prev hash",
			exp:  1,
			opns: opinions(types.RandomHash()),
			err:  errNoValidityAdopted,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := newSyncerWithoutSyncTimer(t)

			require.NoError(t, layers.SetHashes(ts.cdb, gLid, types.RandomHash(), prevHash))
			lid := gLid.Add(1)
			cnt, err := blocks.CountContextualValidity(ts.cdb, lid)
			require.NoError(t, err)
			require.Zero(t, cnt)

			ts.mTortoise.EXPECT().LatestComplete().Return(gLid)
			adopted := tc.opns[tc.exp]
			ts.mCertHdr.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, adopted.Cert).Return(tc.certErr).MaxTimes(1)
			if tc.err == nil {
				ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got []types.BlockID) error {
						require.ElementsMatch(t, append(adopted.Valid, adopted.Invalid...), got)
						for _, bid := range got {
							require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: lid})))
						}
						return nil
					})
			} else if tc.fetchErr != nil {
				ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(tc.fetchErr).Times(len(tc.opns))
			}
			require.ErrorIs(t, ts.syncer.adopt(context.TODO(), lid, tc.opns), tc.err)
			if tc.err != nil {
				need, err := ts.syncer.needCert(logtest.New(t), lid)
				require.NoError(t, err)
				require.True(t, need)
				need, err = ts.syncer.needValidity(logtest.New(t), lid)
				require.NoError(t, err)
				require.True(t, need)
			} else {
				exp := make(map[types.BlockID]bool)
				for _, bid := range adopted.Valid {
					exp[bid] = true
				}
				for _, bid := range adopted.Invalid {
					exp[bid] = false
				}
				bvs, err := blocks.ContextualValidity(ts.cdb, lid)
				require.NoError(t, err)
				require.Len(t, bvs, len(adopted.Valid)+len(adopted.Invalid))
				for _, bv := range bvs {
					require.Equal(t, exp[bv.ID], bv.Validity)
				}
			}
		})
	}
}
