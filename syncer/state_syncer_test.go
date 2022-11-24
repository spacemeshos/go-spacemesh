package syncer

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
)

func opinions(prevHash types.Hash32) []*fetch.LayerOpinion {
	return []*fetch.LayerOpinion{
		{
			PrevAggHash: prevHash,
		},
		{
			PrevAggHash: prevHash,
			Cert: &types.Certificate{
				BlockID: types.RandomBlockID(),
			},
		},
	}
}

func TestProcessLayers_MultiLayers(t *testing.T) {
	gLid := types.GetEffectiveGenesis()
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	current := gLid.Add(10)
	ts.syncer.setLastSyncedLayer(current.Sub(1))
	ts.mTicker.advanceToLayer(current)

	ts.mForkFinder.EXPECT().UpdateAgreement(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	adopted := make(map[types.LayerID]types.BlockID)
	for lid := gLid.Add(1); lid.Before(current); lid = lid.Add(1) {
		lid := lid
		ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
		ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
		ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).DoAndReturn(
			func(context.Context, types.LayerID) ([]*fetch.LayerOpinion, error) {
				prevLid := lid.Sub(1)
				prevHash, err := layers.GetAggregatedHash(ts.cdb, prevLid)
				require.NoError(t, err)
				opns := opinions(prevHash)
				adopted[lid] = opns[1].Cert.BlockID
				return opns, nil
			})
		ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got []types.BlockID) error {
				require.Equal(t, []types.BlockID{adopted[lid]}, got)
				for _, bid := range got {
					require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(bid, types.InnerBlock{LayerIndex: lid})))
				}
				return nil
			})
		ts.mCertHdr.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ types.LayerID, gotC *types.Certificate) error {
				require.Equal(t, adopted[lid], gotC.BlockID)
				require.NoError(t, certificates.Add(ts.cdb, lid, gotC))
				return nil
			})
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().Updates().Return(nil)
		ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
		ts.mConState.EXPECT().ApplyLayer(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got *types.Block) error {
				require.Equal(t, adopted[lid], got.ID())
				return nil
			})
		ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
	}
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_OpinionsNotAdopted(t *testing.T) {
	gLid := types.GetEffectiveGenesis()
	prevHash := types.RandomHash()
	lid := gLid.Add(1)
	tt := []struct {
		name              string
		opns              []*fetch.LayerOpinion
		localCert         types.BlockID
		certErr, fetchErr error
	}{
		{
			name:      "node already has cert",
			opns:      opinions(prevHash),
			localCert: types.RandomBlockID(),
		},
		{
			name: "no certs available",
			opns: []*fetch.LayerOpinion{
				{PrevAggHash: prevHash},
				{PrevAggHash: prevHash},
			},
		},
		{
			name: "cert not accepted",
			opns: []*fetch.LayerOpinion{
				{PrevAggHash: prevHash},
				{PrevAggHash: prevHash, Cert: &types.Certificate{BlockID: types.RandomBlockID()}},
			},
			certErr: errors.New("meh"),
		},
		{
			name: "cert block failed fetching",
			opns: []*fetch.LayerOpinion{
				{PrevAggHash: prevHash},
				{PrevAggHash: prevHash, Cert: &types.Certificate{BlockID: types.RandomBlockID()}},
			},
			fetchErr: errors.New("meh"),
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := newSyncerWithoutSyncTimer(t)
			require.NoError(t, layers.SetHashes(ts.cdb, gLid, types.Hash32{}, prevHash))
			ts.syncer.setATXSynced()
			current := lid.Add(1)
			ts.syncer.setLastSyncedLayer(current.Sub(1))
			ts.mTicker.advanceToLayer(current)

			hasCert := false
			for _, opn := range tc.opns {
				if opn.Cert != nil {
					hasCert = true
				}
			}

			// saves opinions
			if tc.localCert != types.EmptyBlockID {
				require.NoError(t, blocks.Add(ts.cdb, types.NewExistingBlock(tc.localCert, types.InnerBlock{LayerIndex: lid})))
				require.NoError(t, certificates.Add(ts.cdb, lid, &types.Certificate{BlockID: tc.localCert}))
				require.NoError(t, blocks.SetValid(ts.cdb, tc.localCert))
				ts.mConState.EXPECT().ApplyLayer(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got *types.Block) error {
						require.Equal(t, tc.localCert, got.ID())
						return nil
					})
				ts.mConState.EXPECT().GetStateRoot().Return(types.Hash32{}, nil)
			}
			ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
			ts.mLyrPatrol.EXPECT().IsHareInCharge(lid).Return(false)
			ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lid).Return(tc.opns, nil)
			if tc.localCert == types.EmptyBlockID && hasCert {
				ts.mDataFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, got []types.BlockID) error {
						require.Equal(t, []types.BlockID{tc.opns[1].Cert.BlockID}, got)
						return tc.fetchErr
					})
				if tc.fetchErr == nil {
					ts.mCertHdr.EXPECT().HandleSyncedCertificate(gomock.Any(), lid, tc.opns[1].Cert).Return(tc.certErr)
				}
			}
			ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
			if tc.localCert == types.EmptyBlockID {
				ts.mTortoise.EXPECT().LatestComplete().Return(gLid)
			}
			ts.mTortoise.EXPECT().Updates().Return(nil)

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
	ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), lastSynced).Return(nil, nil)
	ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lastSynced)
	ts.mTortoise.EXPECT().Updates().Return(nil)
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
		ts.mTortoise.EXPECT().Updates().Return(nil)
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
	ts.mTortoise.EXPECT().Updates().Return(nil)
	ts.mTortoise.EXPECT().LatestComplete().Return(types.GetEffectiveGenesis())
	require.False(t, ts.syncer.stateSynced())
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
	require.True(t, ts.syncer.stateSynced())
}

func TestProcessLayers_MeshHashDiverged(t *testing.T) {
	ts := newSyncerWithoutSyncTimer(t)
	ts.syncer.setATXSynced()
	current := types.GetEffectiveGenesis().Add(131)
	ts.mTicker.advanceToLayer(current)
	for lid := types.GetEffectiveGenesis().Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.msh.SetZeroBlockLayer(context.TODO(), lid)
		ts.mTortoise.EXPECT().OnHareOutput(lid, types.EmptyBlockID)
		ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), lid)
		ts.mTortoise.EXPECT().Updates().Return(nil)
		ts.mTortoise.EXPECT().LatestComplete().Return(lid.Sub(1))
		ts.mConState.EXPECT().GetStateRoot().Return(types.RandomHash(), nil)
		require.NoError(t, ts.msh.ProcessLayerPerHareOutput(context.TODO(), lid, types.EmptyBlockID))
	}
	instate := ts.syncer.mesh.LatestLayerInState()
	require.Equal(t, current.Sub(1), instate)
	ts.syncer.setLastSyncedLayer(instate)
	prevHash, err := layers.GetAggregatedHash(ts.cdb, instate.Sub(1))
	require.NoError(t, err)
	numPeers := 7
	opns := make([]*fetch.LayerOpinion, 0, numPeers)
	eds := make([]*fetch.EpochData, 0, numPeers)
	for i := 0; i < numPeers; i++ {
		opn := &fetch.LayerOpinion{PrevAggHash: types.RandomHash()}
		opn.SetPeer(p2p.Peer(strconv.Itoa(i)))
		opns = append(opns, opn)
		ed := &fetch.EpochData{
			AtxIDs: types.RandomActiveSet(11),
		}
		eds = append(eds, ed)
	}
	opns[1].PrevAggHash = prevHash
	// node will engage hash resolution with p0 and p2 because
	// p1 has the same mesh hash as node
	// p3's ATXs are not available,
	// p4 failed epoch info query
	// p5 failed fork finding sessions
	// p6 already had a fork-finding session from previous runs.
	epoch := instate.GetEpoch()
	errUnknown := errors.New("unknown")

	ts.mBeacon.EXPECT().GetBeacon(gomock.Any()).Return(types.RandomBeacon(), nil)
	ts.mLyrPatrol.EXPECT().IsHareInCharge(instate).Return(false)
	ts.mDataFetcher.EXPECT().PollLayerOpinions(gomock.Any(), instate).Return(opns, nil)
	ts.mForkFinder.EXPECT().UpdateAgreement(opns[1].Peer(), instate.Sub(1), prevHash, gomock.Any())
	for i := 0; i < numPeers; i++ {
		if i == 1 {
			continue
		}
		if i == 6 {
			ts.mForkFinder.EXPECT().NeedResync(instate.Sub(1), opns[i].PrevAggHash).Return(false)
		} else {
			ts.mForkFinder.EXPECT().NeedResync(instate.Sub(1), opns[i].PrevAggHash).Return(true)
		}
	}

	ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[0].Peer(), epoch-1).Return(eds[0], nil)
	ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[2].Peer(), epoch-1).Return(eds[2], nil)
	ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[3].Peer(), epoch-1).Return(eds[3], nil)
	ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[4].Peer(), epoch-1).Return(nil, errUnknown)
	ts.mDataFetcher.EXPECT().PeerEpochInfo(gomock.Any(), opns[5].Peer(), epoch-1).Return(eds[5], nil)
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
	fork0 := types.NewLayerID(101)
	fork2 := types.NewLayerID(121)
	ts.mForkFinder.EXPECT().FindFork(gomock.Any(), opns[0].Peer(), instate.Sub(1), opns[0].PrevAggHash).Return(fork0, nil)
	ts.mForkFinder.EXPECT().FindFork(gomock.Any(), opns[2].Peer(), instate.Sub(1), opns[2].PrevAggHash).Return(fork2, nil)
	ts.mForkFinder.EXPECT().FindFork(gomock.Any(), opns[5].Peer(), instate.Sub(1), opns[5].PrevAggHash).Return(types.LayerID{}, errUnknown)
	for lid := fork0.Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid, opns[0].Peer())
	}
	for lid := fork2.Add(1); lid.Before(current); lid = lid.Add(1) {
		ts.mDataFetcher.EXPECT().PollLayerData(gomock.Any(), lid, opns[2].Peer())
	}
	ts.mForkFinder.EXPECT().AddResynced(instate.Sub(1), opns[0].PrevAggHash)
	ts.mForkFinder.EXPECT().AddResynced(instate.Sub(1), opns[2].PrevAggHash)
	ts.mForkFinder.EXPECT().Purge(true)

	ts.mTortoise.EXPECT().TallyVotes(gomock.Any(), instate)
	ts.mTortoise.EXPECT().Updates().Return(nil)
	require.NoError(t, ts.syncer.processLayers(context.TODO()))
}
