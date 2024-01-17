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
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), peers).DoAndReturn(
			func(_ context.Context, peers []p2p.Peer) <-chan fetch.Result {
				result := make(chan fetch.Result, len(peers))
				for _, peer := range peers {
					ids, data := generateMaliciousIDs(t)
					for _, id := range ids {
						td.mIDs.EXPECT().IdentityExists(id).Return(exits, nil)
					}
					result <- fetch.Result{
						Data: data,
						Peer: peer,
					}
				}
				return result
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

func TestDataFetch_PollMaliciousIDs_PeerErrors(t *testing.T) {
	t.Run("malformed data in response", func(t *testing.T) {
		t.Parallel()
		peers := []p2p.Peer{"p0"}
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), peers).DoAndReturn(
			func(_ context.Context, peers []p2p.Peer) <-chan fetch.Result {
				result := make(chan fetch.Result, 1)
				result <- fetch.Result{
					Data: []byte("malformed"),
					Peer: peers[0],
				}
				return result
			})
		err := td.PollMaliciousProofs(context.Background())
		require.ErrorContains(t, err, "decode")
	})
	t.Run("peer fails", func(t *testing.T) {
		t.Parallel()
		peers := []p2p.Peer{"p0"}
		expectedErr := errors.New("peer failure")
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), peers).DoAndReturn(
			func(_ context.Context, peers []p2p.Peer) <-chan fetch.Result {
				result := make(chan fetch.Result, 1)
				result <- fetch.Result{
					Err:  expectedErr,
					Peer: peers[0],
				}
				return result
			})
		err := td.PollMaliciousProofs(context.Background())
		require.ErrorIs(t, err, expectedErr)
	})
	t.Run("only second peer sends malformed data (succeed anyway)", func(t *testing.T) {
		t.Parallel()
		peers := []p2p.Peer{"p0", "p1"}
		td := newTestDataFetch(t)
		maliciousIds, data := generateMaliciousIDs(t)
		for _, id := range maliciousIds {
			td.mIDs.EXPECT().IdentityExists(id).Return(true, nil)
		}

		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), peers).DoAndReturn(
			func(_ context.Context, peers []p2p.Peer) <-chan fetch.Result {
				result := make(chan fetch.Result, 2)
				result <- fetch.Result{
					Data: data,
					Peer: peers[0],
				}
				result <- fetch.Result{
					Data: []byte("malformed"),
					Peer: peers[1],
				}
				return result
			})
		td.mFetcher.EXPECT().GetMalfeasanceProofs(gomock.Any(), gomock.Any())
		err := td.PollMaliciousProofs(context.Background())
		require.NoError(t, err)
	})
	t.Run("only second peer fails (succeed anyway)", func(t *testing.T) {
		t.Parallel()
		peers := []p2p.Peer{"p0", "p1"}
		expectedErr := errors.New("peer failure")
		td := newTestDataFetch(t)
		maliciousIds, data := generateMaliciousIDs(t)
		for _, id := range maliciousIds {
			td.mIDs.EXPECT().IdentityExists(id).Return(true, nil)
		}
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetMaliciousIDs(gomock.Any(), peers).DoAndReturn(
			func(_ context.Context, peers []p2p.Peer) <-chan fetch.Result {
				result := make(chan fetch.Result, 2)
				result <- fetch.Result{
					Data: data,
					Peer: peers[0],
				}
				result <- fetch.Result{
					Err:  expectedErr,
					Peer: peers[1],
				}
				return result
			})
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
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID).
			DoAndReturn(func(_ context.Context, _ []p2p.Peer, _ types.LayerID) (<-chan fetch.Result, error) {
				results := make(chan fetch.Result, len(peers))
				for _, peer := range peers {
					td.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
					results <- fetch.Result{Data: generateLayerContent(t), Peer: peer}
				}
				return results, nil
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

func TestDataFetch_PollLayerData_FailToRequest(t *testing.T) {
	t.Parallel()
	peers := GenPeers(3)
	expectedErr := errors.New("failed to request")
	td := newTestDataFetch(t)
	td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
	td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, types.LayerID(7)).Return(nil, expectedErr)
	require.ErrorIs(t, td.PollLayerData(context.Background(), 7), expectedErr)
}

func TestDataFetch_PollLayerData_PeerErrors(t *testing.T) {
	numPeers := 4
	peers := GenPeers(numPeers)
	layerID := types.LayerID(10)
	t.Run("only one peer has data", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID).
			DoAndReturn(func(_ context.Context, _ []p2p.Peer, _ types.LayerID) (<-chan fetch.Result, error) {
				results := make(chan fetch.Result, len(peers))
				td.mFetcher.EXPECT().RegisterPeerHashes(peers[0], gomock.Any())
				results <- fetch.Result{Data: generateLayerContent(t), Peer: peers[0]}
				for i := 1; i < numPeers; i++ {
					results <- fetch.Result{Err: errors.New("not available"), Peer: peers[i]}
				}
				return results, nil
			})
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
		td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
		require.NoError(t, td.PollLayerData(context.TODO(), layerID))
	})
	t.Run("only one peer has empty layer", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID).
			DoAndReturn(func(
				_ context.Context,
				_ []p2p.Peer,
				_ types.LayerID,
			) (<-chan fetch.Result, error) {
				results := make(chan fetch.Result, len(peers))
				results <- fetch.Result{Data: generateEmptyLayer(t), Peer: peers[0]}
				for i := 1; i < numPeers; i++ {
					results <- fetch.Result{Err: errors.New("not available"), Peer: peers[i]}
				}
				return results, nil
			})
		require.NoError(t, td.PollLayerData(context.TODO(), layerID))
	})
	t.Run("one peer sends malformed data", func(t *testing.T) {
		t.Parallel()
		td := newTestDataFetch(t)
		td.mFetcher.EXPECT().SelectBestShuffled(gomock.Any()).Return(peers)
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID).
			DoAndReturn(func(_ context.Context, peers []p2p.Peer, _ types.LayerID) (<-chan fetch.Result, error) {
				results := make(chan fetch.Result, len(peers))
				for _, peer := range peers[:len(peers)-1] {
					td.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
					results <- fetch.Result{Data: generateLayerContent(t), Peer: peer}
				}
				results <- fetch.Result{Data: []byte("malformed"), Peer: peers[len(peers)-1]}
				return results, nil
			})
		td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Times(numPeers - 1)
		td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).MaxTimes(numPeers - 1)
		require.NoError(t, td.PollLayerData(context.Background(), layerID))
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
			td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peers, lid).
				DoAndReturn(func(_ context.Context, _ []p2p.Peer, _ types.LayerID) (<-chan fetch.Result, error) {
					results := make(chan fetch.Result, len(peers))
					for i, peer := range peers {
						if tc.pErrs[i] != nil {
							results <- fetch.Result{Err: tc.pErrs[i], Peer: peer}
						} else {
							if tc.needCert && len(tc.hasCert) > 0 {
								p := peer
								td.mFetcher.EXPECT().RegisterPeerHashes(p, []types.Hash32{tc.hasCert[i].AsHash32()})
							}
							var certified *types.BlockID
							if len(tc.hasCert) > 0 {
								certified = &tc.hasCert[i]
							}
							results <- fetch.Result{Data: generateLayerOpinions2(t, certified), Peer: peer}
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
					return results, nil
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

func TestDataFetch_PollLayerOpinions_FailToRequest(t *testing.T) {
	peers := []p2p.Peer{"p0"}
	td := newTestDataFetch(t)
	expectedErr := errors.New("failed to request")
	td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peers, types.LayerID(10)).Return(nil, expectedErr)
	_, _, err := td.PollLayerOpinions(context.Background(), 10, false, peers)
	require.ErrorIs(t, err, expectedErr)
}

func TestDataFetch_PollLayerOpinions_MalformedData(t *testing.T) {
	peers := []p2p.Peer{"p0"}
	td := newTestDataFetch(t)

	td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peers, types.LayerID(10)).DoAndReturn(
		func(_ context.Context, peers []p2p.Peer, _ types.LayerID) (<-chan fetch.Result, error) {
			result := make(chan fetch.Result, 1)
			result <- fetch.Result{
				Data: []byte("malformed"),
				Peer: peers[0],
			}
			return result, nil
		})
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
