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
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

type testDataFetch struct {
	*syncer.DataFetch
	mMesh     *mocks.MockmeshProvider
	mFetcher  *mocks.Mockfetcher
	mIDs      *mocks.MockidProvider
	mTortoise *smocks.MockTortoise
}

func newTestDataFetch(t *testing.T) *testDataFetch {
	ctrl := gomock.NewController(t)
	lg := logtest.New(t)
	tl := &testDataFetch{
		mMesh:     mocks.NewMockmeshProvider(ctrl),
		mFetcher:  mocks.NewMockfetcher(ctrl),
		mIDs:      mocks.NewMockidProvider(ctrl),
		mTortoise: smocks.NewMockTortoise(ctrl),
	}
	tl.DataFetch = syncer.NewDataFetch(tl.mMesh, tl.mFetcher, tl.mIDs, tl.mTortoise, lg)
	return tl
}

const (
	numBallots   = 10
	numMalicious = 11
)

func generateMaliciousIDs(t *testing.T) []types.NodeID {
	t.Helper()
	malIDs := make([]types.NodeID, numMalicious)
	for i := range malIDs {
		malIDs[i] = types.RandomNodeID()
	}
	return malIDs
}

func generateLayerOpinions(t *testing.T, bid *types.BlockID) []byte {
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
	newTestDataFetchWithMocks := func(_ *testing.T, exists bool) *testDataFetch {
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		for _, peer := range peers {
			td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), peer).DoAndReturn(
				func(_ context.Context, peer p2p.Peer) ([]types.NodeID, error) {
					ids := generateMaliciousIDs(t)
					for _, id := range ids {
						td.mIDs.EXPECT().IdentityExists(id).Return(exists, nil)
					}
					return ids, nil
				})
		}
		return td
	}
	t.Run("getting malfeasance proofs success", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t, true)
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), gomock.Any())
		require.NoError(t, td.PollMaliciousProofs(context.Background()))
	})
	t.Run("getting proofs failure", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t, true)
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), gomock.Any()).Return(errUnknown)
		require.ErrorIs(t, td.PollMaliciousProofs(context.Background()), errUnknown)
	})
	t.Run("ids do not exist", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t, false)
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), nil)
		require.NoError(t, td.PollMaliciousProofs(context.Background()))
	})
}

func TestDataFetch_PollMaliciousIDs_PeerErrors(t *testing.T) {
	t.Run("peer fails", func(t *testing.T) {
		t.Parallel()
		peers := []p2p.Peer{"p0"}
		expectedErr := errors.New("peer failure")
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), p2p.Peer("p0")).Return(nil, expectedErr)
		err := td.PollMaliciousProofs(context.Background())
		require.ErrorIs(t, err, expectedErr)
	})
	t.Run("one peer fails (succeed anyway)", func(t *testing.T) {
		t.Parallel()
		peers := []p2p.Peer{"p0", "p1"}
		expectedErr := errors.New("peer failure")
		td := newTestDataFetch(t)
		maliciousIds := generateMaliciousIDs(t)
		for _, id := range maliciousIds {
			td.mIDs.EXPECT().IdentityExists(id).Return(true, nil)
		}
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), p2p.Peer("p0")).Return(maliciousIds, nil)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), p2p.Peer("p1")).Return(nil, expectedErr)
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), gomock.Any())
		err := td.PollMaliciousProofs(context.Background())
		require.NoError(t, err)
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
		for _, peer := range peers {
			td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peer, layerID).Return(generateLayerContent(t), nil)
			td.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
		}
		return td
	}
	t.Run("all peers have layer data", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t)
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any())
		require.NoError(t, td.PollLayerData(context.Background(), layerID))
	})
	t.Run("GetBallots failure", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetchWithMocks(t)
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(errUnknown)
		require.ErrorIs(t, td.PollLayerData(context.Background(), layerID), errUnknown)
	})
}

func TestDataFetch_PollLayerData_FailToRequest(t *testing.T) {
	t.Parallel()
	peers := GenPeers(3)
	expectedErr := errors.New("failed to request")
	td := newTestDataFetch(t)
	td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
	for _, peer := range peers {
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peer, types.LayerID(7)).Return(nil, expectedErr)
	}
	require.ErrorIs(t, td.PollLayerData(context.Background(), 7), expectedErr)
}

func TestDataFetch_PollLayerData_PeerErrors(t *testing.T) {
	numPeers := 4
	peers := GenPeers(numPeers)
	lid := types.LayerID(10)
	t.Run("only one peer has data", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().RegisterPeerHashes(peers[0], gomock.Any())
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers[0], lid).Return(generateLayerContent(t), nil)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), gomock.Any(), lid).Return(nil, errors.New("na")).Times(numPeers - 1)
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any())
		require.NoError(t, td.PollLayerData(context.Background(), lid))
	})
	t.Run("only one peer has empty layer", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers[0], lid).Return(generateEmptyLayer(t), nil)
		for i := 1; i < numPeers; i++ {
			td.mFetcher.EXPECT().RegisterPeerHashes(peers[i], gomock.Any())
			td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers[i], lid).Return(generateLayerContent(t), nil)
		}
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any())
		require.NoError(t, td.PollLayerData(context.Background(), lid))
	})
	t.Run("one peer sends malformed data", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers[0], lid).Return([]byte("malformed"), nil)
		for i := 1; i < numPeers; i++ {
			td.mFetcher.EXPECT().RegisterPeerHashes(peers[i], gomock.Any())
			td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers[i], lid).Return(generateLayerContent(t), nil)
		}
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any())
		require.NoError(t, td.PollLayerData(context.Background(), lid))
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
			for i, peer := range peers {
				if tc.pErrs[i] != nil {
					td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peer, lid).Return(nil, tc.pErrs[i])
				} else {
					if tc.needCert && len(tc.hasCert) > 0 {
						td.mFetcher.EXPECT().RegisterPeerHashes(peer, []types.Hash32{tc.hasCert[i].AsHash32()})
					}
					var certified *types.BlockID
					if len(tc.hasCert) > 0 {
						certified = &tc.hasCert[i]
					}
					op := generateLayerOpinions(t, certified)
					td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peer, lid).Return(op, nil)
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

			got, certs, err := td.PollLayerOpinions(context.Background(), lid, tc.needCert, peers)
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

func TestDataFetch_PollLayerOpinions_FailToRequest(t *testing.T) {
	peers := []p2p.Peer{"p0"}
	td := newTestDataFetch(t)
	expectedErr := errors.New("failed to request")
	td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peers[0], types.LayerID(10)).Return(nil, expectedErr)
	_, _, err := td.PollLayerOpinions(context.Background(), 10, false, peers)
	require.ErrorIs(t, err, expectedErr)
}

func TestDataFetch_PollLayerOpinions_MalformedData(t *testing.T) {
	peers := []p2p.Peer{"p0"}
	td := newTestDataFetch(t)
	td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peers[0], types.LayerID(10)).Return([]byte("malformed"), nil)
	_, _, err := td.PollLayerOpinions(context.Background(), 10, false, peers)
	require.ErrorContains(t, err, "decode")
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
				td.mTortoise.EXPECT().GetMissingActiveSet(epoch+1, ed.AtxIDs).Return(ed.AtxIDs[1:])
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
