package syncer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
)

type testDataFetch struct {
	*syncer.DataFetch
	cdb      *datastore.CachedDB
	mMesh    *mocks.MockmeshProvider
	mFetcher *mocks.Mockfetcher
}

func newTestDataFetch(t *testing.T) *testDataFetch {
	ctrl := gomock.NewController(t)
	lg := logtest.New(t)
	tl := &testDataFetch{
		cdb:      datastore.NewCachedDB(sql.InMemory(), lg),
		mMesh:    mocks.NewMockmeshProvider(ctrl),
		mFetcher: mocks.NewMockfetcher(ctrl),
	}
	tl.DataFetch = syncer.NewDataFetch(tl.cdb, tl.mMesh, tl.mFetcher, lg)
	return tl
}

const (
	numBallots = 10
	numBlocks  = 3
)

func generateEpochData(t *testing.T) ([]types.ATXID, []byte) {
	t.Helper()
	atxIDs := types.RandomActiveSet(11)
	data, err := codec.EncodeSlice(atxIDs)
	require.NoError(t, err)
	return atxIDs, data
}

func generateLayerOpinions(t *testing.T) []byte {
	t.Helper()
	var lo fetch.LayerOpinion
	lo.Cert = &types.Certificate{
		BlockID: types.RandomBlockID(),
	}
	data, err := codec.Encode(&lo)
	require.NoError(t, err)
	return data
}

func generateLayerContent(t *testing.T) []byte {
	t.Helper()
	ballotIDs := make([]types.BallotID, 0, numBallots)
	for i := 0; i < numBallots; i++ {
		ballotIDs = append(ballotIDs, types.RandomBallotID())
	}
	blockIDs := make([]types.BlockID, 0, numBlocks)
	for i := 0; i < numBlocks; i++ {
		blockIDs = append(blockIDs, types.RandomBlockID())
	}
	lb := fetch.LayerData{
		Ballots: ballotIDs,
		Blocks:  blockIDs,
	}
	out, _ := codec.Encode(&lb)
	return out
}

func generateEmptyLayer() []byte {
	lb := fetch.LayerData{
		Ballots: []types.BallotID{},
		Blocks:  []types.BlockID{},
	}
	out, _ := codec.Encode(&lb)
	return out
}

func GenPeers(num int) []p2p.Peer {
	peers := make([]p2p.Peer, 0, num)
	for i := 0; i < num; i++ {
		peers = append(peers, p2p.Peer(fmt.Sprintf("peer_%d", i)))
	}
	return peers
}

func Test_PollLayerData(t *testing.T) {
	numPeers := 4
	peers := GenPeers(numPeers)
	layerID := types.NewLayerID(10)
	errUnknown := errors.New("unknown")
	tt := []struct {
		name                   string
		zeroBlock              bool
		ballotFail, blocksFail bool
		err                    error
	}{
		{
			name: "all peers have layer data",
		},
		{
			name:      "all peers have zero blocks",
			zeroBlock: true,
		},
		{
			name:       "ballots failure ignored",
			ballotFail: true,
		},
		{
			name:       "blocks failure ignored",
			blocksFail: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			td := newTestDataFetch(t)
			td.mFetcher.EXPECT().GetPeers().Return(peers)
			td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ []p2p.Peer, _ types.LayerID, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
					for _, peer := range peers {
						if tc.zeroBlock {
							okCB(generateEmptyLayer(), peer)
						} else {
							td.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
							okCB(generateLayerContent(t), peer)
						}
					}
					return nil
				})
			if tc.ballotFail {
				td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(errUnknown)
				td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers - 1)
				td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
			} else if tc.blocksFail {
				td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
				td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(errUnknown)
				td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers - 1)
			} else if !tc.zeroBlock {
				td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
				td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
			}
			if tc.zeroBlock {
				td.mMesh.EXPECT().SetZeroBlockLayer(gomock.Any(), layerID)
			}

			require.ErrorIs(t, td.PollLayerData(context.TODO(), layerID), tc.err)
		})
	}
}

func Test_PollLayerData_PeerErrors(t *testing.T) {
	numPeers := 4
	peers := GenPeers(numPeers)
	layerID := types.NewLayerID(10)
	err := errors.New("not available")
	tt := []struct {
		name      string
		errs      []error
		responses [][]byte
		zeroBlock bool
	}{
		{
			name: "only one peer has data",
			errs: []error{err, nil, err, err},
		},
		{
			name:      "only one peer has empty layer",
			errs:      []error{err, nil, err, err},
			zeroBlock: true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Len(t, tc.errs, numPeers)
			td := newTestDataFetch(t)
			td.mFetcher.EXPECT().GetPeers().Return(peers)
			td.mFetcher.EXPECT().GetLayerData(gomock.Any(), peers, layerID, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ []p2p.Peer, _ types.LayerID, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
					for i, peer := range peers {
						if tc.errs[i] == nil {
							if tc.zeroBlock {
								okCB(generateEmptyLayer(), peer)
							} else {
								td.mFetcher.EXPECT().RegisterPeerHashes(peer, gomock.Any())
								okCB(generateLayerContent(t), peer)
							}
						} else {
							errCB(errors.New("not available"), peer)
						}
					}
					return nil
				})
			if tc.zeroBlock {
				td.mMesh.EXPECT().SetZeroBlockLayer(gomock.Any(), layerID)
			} else {
				td.mFetcher.EXPECT().GetBallots(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
				td.mFetcher.EXPECT().GetBlocks(gomock.Any(), gomock.Any()).Return(nil).MaxTimes(numPeers)
			}

			require.NoError(t, td.PollLayerData(context.TODO(), layerID))
		})
	}
}

func Test_PollLayerOpinions(t *testing.T) {
	const numPeers = 4
	peers := GenPeers(numPeers)
	lid := types.NewLayerID(10)
	pe := errors.New("meh")
	tt := []struct {
		name  string
		err   error
		pErrs []error
	}{
		{
			name:  "all peers",
			pErrs: []error{nil, nil, nil, nil},
		},
		{
			name:  "some peers have errors",
			pErrs: []error{pe, pe, nil, nil},
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
			td.mFetcher.EXPECT().GetPeers().Return(peers)
			td.mFetcher.EXPECT().GetLayerOpinions(gomock.Any(), peers, lid, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ []p2p.Peer, _ types.LayerID, okCB func([]byte, p2p.Peer), errCB func(error, p2p.Peer)) error {
					for i, peer := range peers {
						if tc.pErrs[i] != nil {
							errCB(tc.pErrs[i], peer)
						} else {
							okCB(generateLayerOpinions(t), peer)
						}
					}
					return nil
				})

			got, err := td.PollLayerOpinions(context.TODO(), lid)
			require.ErrorIs(t, err, tc.err)
			if err == nil {
				require.NotEmpty(t, got)
			}
		})
	}
}

func Test_GetEpochATXs(t *testing.T) {
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
			name:   "get ID failure",
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
			td.mFetcher.EXPECT().GetPeers().Return(peers)
			td.mFetcher.EXPECT().GetEpochATXIDs(gomock.Any(), gomock.Any(), epoch, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, peer p2p.Peer, _ types.EpochID, okCB func([]byte), errCB func(error)) error {
					require.Contains(t, peers, peer)
					if tc.getErr != nil {
						errCB(tc.getErr)
					} else {
						atxIDs, data := generateEpochData(t)
						okCB(data)
						td.mFetcher.EXPECT().RegisterPeerHashes(peer, types.ATXIDsToHashes(atxIDs))
						td.mFetcher.EXPECT().GetAtxs(gomock.Any(), atxIDs).Return(tc.fetchErr)
					}
					return nil
				})
			require.ErrorIs(t, td.GetEpochATXs(context.TODO(), epoch), tc.err)
		})
	}
}
