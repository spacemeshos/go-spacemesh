package syncer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
)

type testDataFetch struct {
	*syncer.DataFetch
	mMesh     *mocks.MockmeshProvider
	mFetcher  *mocks.Mockfetcher
	mIDs      *mocks.MockidProvider
	mAtxCache *mocks.MockactiveSetCache
}

func newTestDataFetch(t *testing.T) *testDataFetch {
	ctrl := gomock.NewController(t)
	lg := logtest.New(t)
	tl := &testDataFetch{
		mMesh:     mocks.NewMockmeshProvider(ctrl),
		mFetcher:  mocks.NewMockfetcher(ctrl),
		mIDs:      mocks.NewMockidProvider(ctrl),
		mAtxCache: mocks.NewMockactiveSetCache(ctrl),
	}
	tl.DataFetch = syncer.NewDataFetch(tl.mMesh, tl.mFetcher, tl.mIDs, tl.mAtxCache, lg)
	return tl
}

const (
	numBallots   = 10
	numMalicious = 11
)

func generateMaliciousIDs(t *testing.T) ([]types.NodeID, []byte) {
	t.Helper()
	var malicious fetch.MaliciousIDs
	for i := 0; i < numMalicious; i++ {
		malicious.NodeIDs = append(malicious.NodeIDs, types.RandomNodeID())
	}
	data, err := codec.Encode(&malicious)
	require.NoError(t, err)
	return malicious.NodeIDs, data
}

func generateLayerOpinions2(t *testing.T, bid *types.BlockID) []byte {
	t.Helper()
	lo := &fetch.LayerOpinion{
		PrevAggHash: types.RandomHash(),
		Certified:   bid,
	}
	data, err := codec.Encode(lo)
	require.NoError(t, err)
	return data
}

func generateLayerContent(t *testing.T) []byte {
	t.Helper()
	ballotIDs := make([]types.BallotID, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballotIDs = append(ballotIDs, types.RandomBallotID())
	}
	lb := fetch.LayerData{
		Ballots: ballotIDs,
	}
	out, err := codec.Encode(&lb)
	require.NoError(t, err)
	return out
}

func generateEmptyLayer(t *testing.T) []byte {
	lb := fetch.LayerData{
		Ballots: []types.BallotID{},
	}
	out, err := codec.Encode(&lb)
	require.NoError(t, err)
	return out
}

func GenPeers(num int) []p2p.Peer {
	peers := make([]p2p.Peer, 0, num)
	for i := 0; i < num; i++ {
		peers = append(peers, p2p.Peer(fmt.Sprintf("peer_%d", i)))
	}
	return peers
}

func TestDataFetch_PollMaliciousIDs(t *testing.T) {
	numPeers := 4
	peers := GenPeers(numPeers)
	errUnknown := errors.New("unknown")
	newTestDataFetchWithMocks := func(_ *testing.T, exits bool) *testDataFetch {
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), peers, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ []p2p.Peer, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
				for _, peer := range peers {
					ids, data := generateMaliciousIDs(t)
					for _, id := range ids {
						td.mIDs.EXPECT().IdentityExists(id).Return(exits, nil)
					}
					okCB(data, peer)
				}
				return nil
			})
		return td
	}
	t.Run("all peers have malfeasance proofs", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t, true)
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
		require.NoError(t, td.PollMaliciousProofs(context.TODO()))
	})
	t.Run("proof failure ignored", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t, true)
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), gomock.Any()).Return(errUnknown)
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers - 1)
		require.NoError(t, td.PollMaliciousProofs(context.TODO()))
	})
	t.Run("ids do not exist", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t, false)
		require.NoError(t, td.PollMaliciousProofs(context.TODO()))
	})
}

func TestDataFetch_PollLayerData(t *testing.T) {
	numPeers := 4
	peers := GenPeers(numPeers)
	layerID := types.LayerID(10)
	errUnknown := errors.New("unknown")
	newTestDataFetchWithMocks := func(*testing.T) *testDataFetch {
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID, gomock.Any(), gomock.Any()).
			DoAndReturn(func(
				_ context.Context,
				_ []p2p.Peer,
				_ types.LayerID,
				okCB func([]byte, p2p.Peer),
				errCB func(error, p2p.Peer),
			) error {
				for _, peer := range peers {
					td.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
					okCB(generateLayerContent(t), peer)
				}
				return nil
			})
		return td
	}
	t.Run("all peers have layer data", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t)
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
		require.NoError(t, td.PollLayerData(context.TODO(), layerID))
	})
	t.Run("ballots failure ignored", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t)
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(errUnknown)
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers - 1)
		require.NoError(t, td.PollLayerData(context.TODO(), layerID))
	})
	t.Run("blocks failure ignored", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t)
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
		require.NoError(t, td.PollLayerData(context.TODO(), layerID))
	})
}

func TestDataFetch_PollLayerData_PeerErrors(t *testing.T) {
	numPeers := 4
	peers := GenPeers(numPeers)
	layerID := types.LayerID(10)
	t.Run("only one peer has data", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID, gomock.Any(), gomock.Any()).
			DoAndReturn(func(
				_ context.Context,
				_ []p2p.Peer,
				_ types.LayerID,
				okCB func([]byte, p2p.Peer),
				errCB func(error, p2p.Peer),
			) error {
				td.mFetcher.EXPECT().RegisterPeerHashes(peers[0], gomock.Any())
				okCB(generateLayerContent(t), peers[0])
				for i := 1; i < numPeers; i++ {
					errCB(errors.New("not available"), peers[i])
				}
				return nil
			})
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
		td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
		require.NoError(t, td.PollLayerData(context.TODO(), layerID))
	})
	t.Run("only one peer has empty layer", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID, gomock.Any(), gomock.Any()).
			DoAndReturn(func(
				_ context.Context,
				_ []p2p.Peer,
				_ types.LayerID,
				okCB func([]byte, p2p.Peer),
				errCB func(error, p2p.Peer),
			) error {
				okCB(generateEmptyLayer(t), peers[0])
				for i := 1; i < numPeers; i++ {
					errCB(errors.New("not available"), peers[i])
				}
				return nil
			})
		require.NoError(t, td.PollLayerData(context.TODO(), layerID))
	})
}

func TestDataFetch_PollLayerOpinions(t *testing.T) {
	const numPeers = 4
	peers := GenPeers(numPeers)
	lid := types.LayerID(10)
	pe := errors.New("meh")
	tt := []struct {
		name                      string
		needCert                  bool
		err, cErr                 error
		hasCert, queried, expCert []types.BlockID
		pErrs                     []error
	}{
		{
			name:    "all peers",
			pErrs:   []error{nil, nil, nil, nil},
			hasCert: []types.BlockID{{1}, {2}, {3}, {4}},
		},
		{
			name:     "all peers need cert",
			pErrs:    []error{nil, nil, nil, nil},
			hasCert:  []types.BlockID{{1}, {1}, {2}, {2}},
			needCert: true,
			queried:  []types.BlockID{{1}, {2}},
			expCert:  []types.BlockID{{1}, {2}},
		},
		{
			name:     "all peers need cert but cert error",
			pErrs:    []error{nil, nil, nil, nil},
			cErr:     pe,
			hasCert:  []types.BlockID{{1}, {1}, {2}, {2}},
			needCert: true,
			queried:  []types.BlockID{{1}, {2}},
		},
		{
			name:     "all peers need cert but not available",
			pErrs:    []error{nil, nil, nil, nil},
			needCert: true,
		},
		{
			name:     "some peers have errors",
			pErrs:    []error{pe, pe, nil, nil},
			hasCert:  []types.BlockID{{1}, {1}, {1}, {1}},
			needCert: true,
			queried:  []types.BlockID{{1}},
			expCert:  []types.BlockID{{1}},
		},
		{
			name:  "all peers have errors",
			pErrs: []error{pe, pe, pe, pe},
			err:   pe,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			td := newTestDataFetch(t)
			td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peers, lid, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ []p2p.Peer, _ types.LayerID, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
					for i, peer := range peers {
						if tc.pErrs[i] != nil {
							errCB(tc.pErrs[i], peer)
						} else {
							if tc.needCert && len(tc.hasCert) > 0 {
								p := peer
								td.mFetcher.EXPECT().RegisterPeerHashes(p, []types.Hash32{tc.hasCert[i].AsHash32()})
							}
							var certified *types.BlockID
							if len(tc.hasCert) > 0 {
								certified = &tc.hasCert[i]
							}
							okCB(generateLayerOpinions2(t, certified), peer)
						}
					}
					for _, bid := range tc.queried {
						td.mFetcher.EXPECT().GetCert(gomock.Any(), lid, bid, gomock.Any()).DoAndReturn(
							func(_ context.Context, _ types.LayerID, bid types.BlockID, peers []p2p.Peer) (*types.Certificate, error) {
								require.Len(t, peers, 2)
								if tc.cErr == nil {
									return &types.Certificate{BlockID: bid}, nil
								} else {
									return nil, tc.cErr
								}
							})
					}
					return nil
				})

			got, certs, err := td.PollLayerOpinions(context.TODO(), lid, tc.needCert, peers)
			require.ErrorIs(t, err, tc.err)
			if err == nil {
				require.NotEmpty(t, got)
				if tc.needCert {
					var got []types.BlockID
					for _, cert := range certs {
						got = append(got, cert.BlockID)
					}
					require.ElementsMatch(t, tc.expCert, got)
				}
			}
		})
	}
}

func TestDataFetch_GetEpochATXs(t *testing.T) {
	const numPeers = 4
	peers := GenPeers(numPeers)
	epoch := types.EpochID(11)
	errGetID := errors.New("err get id")
	errFetch := errors.New("err fetch")
	tt := []struct {
		name                  string
		err, getErr, fetchErr error
	}{
		{
			name: "success",
		},
		{
			name:   "get failure",
			getErr: errGetID,
			err:    errGetID,
		},
		{
			name:     "fetch failure",
			fetchErr: errFetch,
			err:      errFetch,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			td := newTestDataFetch(t)
			ed := &fetch.EpochData{
				AtxIDs: types.RandomActiveSet(11),
			}
			td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
			if tc.getErr == nil {
				td.mAtxCache.EXPECT().GetMissingActiveSet(epoch+1, ed.AtxIDs).Return(ed.AtxIDs[1:])
			}
			td.mFetcher.EXPECT().PeerEpochInfo(gomock.Any(), gomock.Any(), epoch).DoAndReturn(
				func(_ context.Context, peer p2p.Peer, _ types.EpochID) (*fetch.EpochData, error) {
					require.Contains(t, peers, peer)
					if tc.getErr != nil {
						return nil, tc.getErr
					} else {
						td.mFetcher.EXPECT().RegisterPeerHashes(peer, types.ATXIDsToHashes(ed.AtxIDs))
						td.mFetcher.EXPECT().GetAtxs(gomock.Any(), ed.AtxIDs[1:]).Return(tc.fetchErr)
						return ed, nil
					}
				})
			require.ErrorIs(t, td.GetEpochATXs(context.TODO(), epoch), tc.err)
		})
	}
}
