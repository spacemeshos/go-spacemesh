package syncer

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func opinions(prevHash types.Hash32) []*fetch.LayerOpinion {
	opns := []*fetch.LayerOpinion{
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
	for _, opn := range opns {
		opn.Cert = &types.Certificate{
			BlockID: opn.Valid[0],
		}
	}
	return opns
}

func uniqueBlockIDs(opn *fetch.LayerOpinion) []types.BlockID {
	var bids []types.BlockID
	if opn.Cert != nil {
		bids = append(bids, opn.Cert.BlockID)
	}
	bids = append(bids, opn.Valid...)
	bids = append(bids, opn.Invalid...)
	return util.UniqueSliceStringer(bids)
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
		hasCert, hasValidity             bool
	}{
		{
			name:        "all good",
			hasCert:     true,
			hasValidity: true,
		},
		{
			name:        "cert missing",
			hasValidity: true,
		},
		{
			name:    "validity missing",
			hasCert: true,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.True(t, tc.hasValidity || tc.hasCert)
			ts := newSyncerWithoutSyncTimer(t)
			ts.mForkFinder.EXPECT().UpdateAgreement(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			ts.syncer.setATXSynced()
			current := gLid.Add(10)
			ts.syncer.setLastSyncedLayer(current.Sub(1))
			ts.mTicker.advanceToLayer(current)
			ts.mDataFetcher.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any()).AnyTimes()
			layerOpns := make(map[types.LayerID][]*fetch.LayerOpinion)
			adopted := make(map[types.LayerID]*fetch.LayerOpinion)
			expectedFetch := make(map[types.LayerID][]types.BlockID)
			actualFetch := make(map[types.LayerID][]types.BlockID)
			for lid := gLid.Add(1); lid.Before(current); lid = lid.Add(1) {
				prevHash := types.RandomHash()
				if lid.Sub(1) == gLid {
					h, err := layers.GetAggregatedHash(ts.cdb, lid.Sub(1))
					require.NoError(t, err)
					prevHash = h
				}
				opns := opinions(prevHash)
				for _, opn := range opns {
					if !tc.hasCert {
						opn.Cert = nil
					}
					if !tc.hasValidity {
						opn.Valid = []types.BlockID{}
						opn.Invalid = []types.BlockID{}
					}
				}
				layerOpns[lid] = opns
				adopted[lid] = opns[1]
				expectedFetch[lid] = []types.BlockID{}
				for _, opn := range opns {
					expectedFetch[lid] = append(expectedFetch[lid], uniqueBlockIDs(opn)...)
				}
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
				ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got []types.BlockID) error {
						if _, ok := actualFetch[lid]; !ok {
							actualFetch[lid] = []types.BlockID{}
						}
						actualFetch[lid] = append(actualFetch[lid], got...)
						for _, bid := range got {
							require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: lid})))
						}
						return nil
					}).Times(len(layerOpns[lid]))
				if tc.hasCert {
					ts.mCertHdr.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, cert).DoAndReturn(
						func(_ context.Context, gotL types.LayerID, gotC *types.Certificate) error {
							require.NoError(t, layers.SetHareOutputWithCert(ts.cdb, gotL, gotC))
							return nil
						})
				}
				ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
				if !tc.hasValidity {
					ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
				}
				ts.mConState.EXPECT().ApplyLayer(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got *types.Block) error {
						if tc.hasValidity {
							types.SortBlockIDs(adopted[lid].Valid)
							require.Equal(t, adopted[lid].Valid[0], got.ID())
						} else {
							require.Equal(t, cert.BlockID, got.ID())
						}
						return nil
					})
				ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
			}
			require.False(t, ts.syncer.stateSynced())
			require.NoError(t, ts.syncer.processLayers(context.TODO()))
			require.True(t, ts.syncer.stateSynced())
			for lid := gLid.Add(1); lid.Before(current); lid = lid.Add(1) {
				require.ElementsMatch(t, expectedFetch[lid], actualFetch[lid])
				if tc.hasValidity {
					checkHasBlockValidity(t, ts.cdb, lid)
				}
			}
		})
	}
}

func TestFetchOpinions_OpinionsNotGood(t *testing.T) {
	gLid := types.GetEffectiveGenesis()
	tt := []struct {
		name          string
		opns          []*fetch.LayerOpinion
		err, fetchErr error
	}{
		{
			name: "all good",
			opns: []*fetch.LayerOpinion{
				{
					Valid:   []types.BlockID{{1, 2, 3}},
					Invalid: []types.BlockID{{2, 3, 4}},
				},
			},
		},
		{
			name: "blocks missing",
			opns: []*fetch.LayerOpinion{
				{
					Valid:   []types.BlockID{{1, 2, 3}},
					Invalid: []types.BlockID{{2, 3, 4}},
				},
			},
			fetchErr: errors.New("unknown"),
			err:      errNoOpinionsAvailable,
		},
		{
			name: "conflicting opinions",
			opns: []*fetch.LayerOpinion{
				{
					Valid:   []types.BlockID{{1, 2, 3}},
					Invalid: []types.BlockID{{1, 2, 3}},
				},
			},
			err: errNoOpinionsAvailable,
		},
		{
			name: "invalid Invalid",
			opns: []*fetch.LayerOpinion{
				{
					Valid:   []types.BlockID{{1, 2, 3}},
					Invalid: []types.BlockID{types.EmptyBlockID},
				},
			},
			err: errNoOpinionsAvailable,
		},
		{
			name: "cert not in validity",
			opns: []*fetch.LayerOpinion{
				{
					Valid: []types.BlockID{{1, 2, 3}},
					Cert: &types.Certificate{
						BlockID: types.BlockID{2, 2, 3},
					},
				},
			},
			err: errNoOpinionsAvailable,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ts := newSyncerWithoutSyncTimer(t)
			ts.syncer.setATXSynced()
			current := gLid.Add(2)
			lid := current.Sub(1)
			ts.syncer.setLastSyncedLayer(lid)
			ts.mTicker.advanceToLayer(current)
			ts.mDataFetcher.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any()).AnyTimes()
			ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(tc.opns, nil)
			if tc.fetchErr != nil {
				ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(tc.fetchErr).AnyTimes()
			} else if tc.err == nil {
				ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			}
			got, err := ts.syncer.fetchOpinions(context.TODO(), ts.syncer.logger, lid)
			require.ErrorIs(t, err, tc.err)
			if tc.err == nil {
				require.ElementsMatch(t, got, tc.opns)
			} else {
				require.Empty(t, got)
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
			ts.mDataFetcher.EXPECT().RegisterPeerHashes(gomock.Any(), gomock.Any()).AnyTimes()

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
			var expectedFetch, actualFetch []types.BlockID
			opns := opinions(types.Hash32{})
			for _, opn := range opns {
				opn.Verified = tc.opnVerified
				opn.Cert = nil
				expectedFetch = append(expectedFetch, uniqueBlockIDs(opn)...)
			}
			ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), tc.requested).Return(opns, nil)
			ts.mTortoise.EXPECT().LatestComplete().Return(tc.verified)
			ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, got []types.BlockID) error {
					actualFetch = append(actualFetch, got...)
					for _, bid := range got {
						require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: tc.requested})))
					}
					return nil
				}).Times(len(opns))
			ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), tc.requested)
			if tc.localOpn == types.EmptyBlockID {
				ts.mTortoise.EXPECT().LatestComplete().Return(tc.requested.Sub(1))
			}
			require.False(t, ts.syncer.stateSynced())
			require.NoError(t, ts.syncer.processLayers(context.TODO()))
			require.True(t, ts.syncer.stateSynced())
			require.ElementsMatch(t, expectedFetch, actualFetch)
			if tc.localOpn == types.EmptyBlockID {
				need, err := ts.syncer.needCert(logtest.New(t), tc.requested)
				require.NoError(t, err)
				require.True(t, need)
				need, err = ts.syncer.needValidity(logtest.New(t), tc.requested)
				require.NoError(t, err)
				require.True(t, need)
			} else {
				bvs, err := blocks.ContextualValidity(ts.cdb, tc.requested)
				require.NoError(t, err)
				require.Len(t, bvs, len(expectedFetch)+1)
				for _, bv := range bvs {
					if bv.ID == tc.localOpn {
						require.True(t, bv.Validity)
					} else {
						require.False(t, bv.Validity)
					}
				}
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
	sorted[2].Cert = nil
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
		name         string
		opns         []*fetch.LayerOpinion
		exp          int
		err, certErr error
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
			ts.mForkFinder.EXPECT().UpdateAgreement(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
			lid := gLid.Add(1)
			numBlocks := 0
			for _, opn := range tc.opns {
				for _, bid := range uniqueBlockIDs(opn) {
					require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: lid})))
					numBlocks++
				}
			}
			cnt, err := blocks.CountContextualValidity(ts.cdb, lid)
			require.NoError(t, err)
			require.Zero(t, cnt)

			ts.mTortoise.EXPECT().LatestComplete().Return(gLid)
			adopted := tc.opns[tc.exp]
			ts.mCertHdr.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, adopted.Cert).Return(tc.certErr).MaxTimes(1)
			require.ErrorIs(t, ts.syncer.adopt(context.TODO(), lid, prevHash, tc.opns), tc.err)
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
				bvs, err := blocks.ContextualValidity(ts.cdb, lid)
				require.NoError(t, err)
				require.Len(t, bvs, numBlocks)
				for _, bv := range bvs {
					if _, ok := exp[bv.ID]; ok {
						require.True(t, bv.Validity)
					} else {
						require.False(t, bv.Validity)
					}
				}
			}
		})
	}
}

func TestMeshAgreement(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	current := types.GetEffectiveGenesis().Add(131)
	ts.mTicker.advanceToLayer(current)
	for lid := types.GetEffectiveGenesis().Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.msh.SetZeroBlockLayer(context.TODO(), lid)
		ts.mTortoise.EXPECT().OnHareOutput(lid, types.EmptyBlockID)
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
		ts.mConState.EXPECT().GetStateRoot().Return(types.RandomHash(), nil)
		require.NoError(t, ts.msh.ProcessLayerPerHareOutput(context.TODO(), lid, types.EmptyBlockID))
	}
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	instate := ts.syncer.mesh.LatestLayerInState()
	prevHash, err := layers.GetAggregatedHash(ts.cdb, instate.Sub(1))
	require.NoError(t, err)
	numPeers := 6
	opns := make([]*fetch.LayerOpinion, 0, numPeers)
	eds := make([]*fetch.EpochData, 0, numPeers)
	for i := 0; i < numPeers; i++ {
		opn := &fetch.LayerOpinion{PrevAggHash: types.RandomHash()}
		opn.SetPeer(p2p.Peer(strconv.Itoa(i)))
		opns = append(opns, opn)
		ed := &fetch.EpochData{
			Beacon: types.RandomBeacon(),
			AtxIDs: types.RandomActiveSet(11),
			Weight: rand.Uint64(),
		}
		eds = append(eds, ed)
	}
	opns[1].PrevAggHash = prevHash
	// node will engage hash resolution with p0 and p2 because
	// p1 has the same mesh hash as node
	// p3's ATXs are not available,
	// p4 failed epoch info query
	// p5 failed fork finding sessions
	epoch := instate.GetEpoch()
	errUnknown := errors.New("unknown")

	t.Run("beacon not available", func(t *testing.T) {
		ts.mBeacon.EXPECT().GetBeacon(epoch).Return(types.RandomBeacon(), errUnknown)
		_, err = ts.syncer.ensureMeshAgreement(context.TODO(), ts.syncer.logger, instate, opns, map[p2p.Peer]struct{}{})
		require.ErrorIs(t, err, errUnknown)
	})

	t.Run("success", func(t *testing.T) {
		ts.mBeacon.EXPECT().GetBeacon(epoch).Return(types.RandomBeacon(), nil)
		ts.mForkFinder.EXPECT().UpdateAgreement(opns[1].Peer(), instate.Sub(1), prevHash, gomock.Any())
		ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[0].Peer(), epoch).Return(eds[0], nil)
		ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[2].Peer(), epoch).Return(eds[2], nil)
		ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[3].Peer(), epoch).Return(eds[3], nil)
		ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[4].Peer(), epoch).Return(nil, errUnknown)
		ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[5].Peer(), epoch).Return(eds[5], nil)
		ts.mDataFetcher.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got []types.ATXID) error {
				require.ElementsMatch(t, eds[0].AtxIDs, got)
				return nil
			},
		)
		ts.mDataFetcher.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got []types.ATXID) error {
				require.ElementsMatch(t, eds[2].AtxIDs, got)
				return nil
			},
		)
		ts.mDataFetcher.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got []types.ATXID) error {
				require.ElementsMatch(t, eds[3].AtxIDs, got)
				return errors.New("not available")
			},
		)
		ts.mDataFetcher.EXPECT().GetAtxs(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got []types.ATXID) error {
				require.ElementsMatch(t, eds[5].AtxIDs, got)
				return nil
			},
		)
		ts.mForkFinder.EXPECT().FindFork(gomock.Any(), opns[0].Peer(), instate.Sub(1), opns[0].PrevAggHash).Return(types.NewLayerID(101), nil)
		ts.mForkFinder.EXPECT().FindFork(gomock.Any(), opns[2].Peer(), instate.Sub(1), opns[2].PrevAggHash).Return(types.NewLayerID(121), nil)
		ts.mForkFinder.EXPECT().FindFork(gomock.Any(), opns[5].Peer(), instate.Sub(1), opns[5].PrevAggHash).Return(types.LayerID{}, errUnknown)
		expected := types.NewLayerID(101).Add(1)
		for lid := expected; lid.Before(current); lid = lid.Add(1) {
			ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid, []p2p.Peer{opns[0].Peer(), opns[2].Peer()})
		}
		ts.mForkFinder.EXPECT().Purge(true)
		got, err := ts.syncer.ensureMeshAgreement(context.TODO(), ts.syncer.logger, instate, opns, map[p2p.Peer]struct{}{})
		require.NoError(t, err)
		require.Equal(t, expected, got)
	})
}
