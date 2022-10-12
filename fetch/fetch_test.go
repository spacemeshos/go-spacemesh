package fetch

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type testFetch struct {
	*Fetch
	mh     *mocks.Mockhost
	mAtxS  *mocks.Mockrequester
	mLyrS  *mocks.Mockrequester
	mOpnS  *mocks.Mockrequester
	mHashS *mocks.Mockrequester

	mMesh      *mocks.MockmeshProvider
	mAtxH      *mocks.MockatxHandler
	mBallotH   *mocks.MockballotHandler
	mBlocksH   *mocks.MockblockHandler
	mProposalH *mocks.MockproposalHandler
	method     int
	mTxH       *mocks.MocktxHandler
	mPoetH     *mocks.MockpoetHandler
}

func createFetch(tb testing.TB) *testFetch {
	ctrl := gomock.NewController(tb)
	tf := &testFetch{
		mh:         mocks.NewMockhost(ctrl),
		mAtxS:      mocks.NewMockrequester(ctrl),
		mLyrS:      mocks.NewMockrequester(ctrl),
		mOpnS:      mocks.NewMockrequester(ctrl),
		mHashS:     mocks.NewMockrequester(ctrl),
		mAtxH:      mocks.NewMockatxHandler(ctrl),
		mBallotH:   mocks.NewMockballotHandler(ctrl),
		mBlocksH:   mocks.NewMockblockHandler(ctrl),
		mProposalH: mocks.NewMockproposalHandler(ctrl),
		mTxH:       mocks.NewMocktxHandler(ctrl),
		mPoetH:     mocks.NewMockpoetHandler(ctrl),
	}
	cfg := Config{
		2000, // make sure we never hit the batch timeout
		3,
		3,
		3,
		3,
	}
	lg := logtest.New(tb)
	tf.Fetch = NewFetch(datastore.NewCachedDB(sql.InMemory(), lg), tf.mMesh, nil,
		WithConfig(cfg),
		WithLogger(lg),
		WithATXHandlers(tf.mAtxH),
		WithBallotHandlers(tf.mBallotH),
		WithBlockHandlers(tf.mBlocksH),
		WithProposalHandlers(tf.mProposalH),
		WithTXHandlers(tf.mTxH),
		WithPoetHandlers(tf.mPoetH),
		withServers(map[string]requester{
			atxProtocol:     tf.mAtxS,
			lyrDataProtocol: tf.mLyrS,
			lyrOpnsProtocol: tf.mOpnS,
			hashProtocol:    tf.mHashS,
		}),
		withHost(tf.mh))

	return tf
}

func TestFetch_GetHash(t *testing.T) {
	f := createFetch(t)
	f.mh.EXPECT().Close()
	f.Start()
	defer f.Stop()
	h1 := types.RandomHash()
	hint := datastore.POETDB
	hint2 := datastore.BallotDB

	// test hash aggregation
	f.GetHash(h1, hint, false)
	f.GetHash(h1, hint, false)

	h2 := types.RandomHash()
	f.GetHash(h2, hint2, false)

	// test aggregation by hint
	f.activeReqM.RLock()
	assert.Equal(t, 2, len(f.activeRequests[h1]))
	f.activeReqM.RUnlock()
}

func TestFetch_RequestHashBatchFromPeers(t *testing.T) {
	tt := []struct {
		name     string
		validate bool
		err      error
	}{
		{
			name:     "request batch hash aggregated",
			validate: false,
		},
		{
			name:     "request batch hash aggregated and validated",
			validate: true,
		},
		{
			name: "request batch hash aggregated network failure",
			err:  errors.New("network failure"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			f.cfg.MaxRetriesForRequest = 0
			f.cfg.MaxRetriesForPeer = 0
			peer := p2p.Peer("buddy")
			f.mh.EXPECT().GetPeers().Return([]p2p.Peer{peer})

			hsh := types.RandomHash()
			res := ResponseMessage{
				Hash: hsh,
				Data: []byte("a"),
			}
			f.mHashS.EXPECT().Request(gomock.Any(), peer, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, req []byte, okFunc func([]byte), _ func(error)) error {
					if tc.err != nil {
						return tc.err
					}
					var rb RequestBatch
					err := codec.Decode(req, &rb)
					require.NoError(t, err)
					resBatch := ResponseBatch{
						ID:        rb.ID,
						Responses: []ResponseMessage{res},
					}
					bts, err := codec.Encode(&resBatch)
					require.NoError(t, err)
					okFunc(bts)
					return nil
				})

			req := request{
				hash:                 hsh,
				validateResponseHash: tc.validate,
				hint:                 datastore.POETDB,
				returnChan:           make(chan HashDataPromiseResult, 3),
			}

			f.activeReqM.Lock()
			f.activeRequests[hsh] = []*request{&req, &req, &req}
			f.activeReqM.Unlock()
			f.requestHashBatchFromPeers()
			close(req.returnChan)
			for x := range req.returnChan {
				if tc.err != nil {
					require.ErrorIs(t, x.Err, tc.err)
				} else if tc.validate {
					require.ErrorIs(t, x.Err, errWrongHash)
				} else {
					require.NoError(t, x.Err)
				}
			}
		})
	}
}

func TestFetch_GetHash_StartStopSanity(t *testing.T) {
	f := createFetch(t)
	f.mh.EXPECT().Close()
	f.Start()
	f.Stop()
}

func TestFetch_Loop_BatchRequestMax(t *testing.T) {
	f := createFetch(t)
	f.cfg.BatchTimeout = 1
	f.cfg.MaxRetriesForPeer = 2
	f.cfg.BatchSize = 2
	peer := p2p.Peer("buddy")
	f.mh.EXPECT().GetPeers().Return([]p2p.Peer{peer})

	h1 := types.RandomHash()
	h2 := types.RandomHash()
	h3 := types.RandomHash()
	f.mHashS.EXPECT().Request(gomock.Any(), peer, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ p2p.Peer, req []byte, okFunc func([]byte), _ func(error)) error {
			var rb RequestBatch
			err := codec.Decode(req, &rb)
			require.NoError(t, err)
			resps := make([]ResponseMessage, 0, len(rb.Requests))
			for _, r := range rb.Requests {
				resps = append(resps, ResponseMessage{
					Hash: r.Hash,
					Data: []byte("a"),
				})
			}
			resBatch := ResponseBatch{
				ID:        rb.ID,
				Responses: resps,
			}
			bts, err := codec.Encode(&resBatch)
			require.NoError(t, err)
			okFunc(bts)
			return nil
		}).Times(2) // 3 requests with batch size 2 -> 2 sends

	hint := datastore.POETDB

	f.mh.EXPECT().Close()
	defer f.Stop()
	f.Start()
	r1 := f.GetHash(h1, hint, false)
	r2 := f.GetHash(h2, hint, false)
	r3 := f.GetHash(h3, hint, false)
	for _, ch := range []chan HashDataPromiseResult{r1, r2, r3} {
		res := <-ch
		require.NoError(t, res.Err)
		require.NotEmpty(t, res.Data)
		require.False(t, res.IsLocal)
	}
}

func TestFetch_GetRandomPeer(t *testing.T) {
	myPeers := make([]p2p.Peer, 1000)
	for i := 0; i < len(myPeers); i++ {
		myPeers[i] = p2p.Peer(types.RandomBytes(20))
	}
	allTheSame := true
	for i := 0; i < 20; i++ {
		peer1 := randomPeer(myPeers)
		peer2 := randomPeer(myPeers)
		if peer1 != peer2 {
			allTheSame = false
		}
	}
	assert.False(t, allTheSame)
}
