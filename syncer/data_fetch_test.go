package syncer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
)

type testDataFetch struct {
	*syncer.DataFetch
	mFetcher *mocks.Mockfetcher
}

func newTestDataFetch(t *testing.T) *testDataFetch {
	ctrl := gomock.NewController(t)
	tl := &testDataFetch{
		mFetcher: mocks.NewMockfetcher(ctrl),
	}
	tl.DataFetch = syncer.NewDataFetch(tl.mFetcher, zaptest.NewLogger(t))
	return tl
}

const (
	numBallots   = 10
	numMalicious = 11
)

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
		td.mFetcher.EXPECT().GetLayerData(gomock.Any(), gomock.Any(), lid).Return(nil, errors.New("na")).
			Times(numPeers - 1)
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
					func(_ context.Context, _ types.LayerID, bid types.BlockID, peers []p2p.Peer,
					) (*types.Certificate, error) {
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
