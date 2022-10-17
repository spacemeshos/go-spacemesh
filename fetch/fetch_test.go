package fetch

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	ftypes "github.com/spacemeshos/go-spacemesh/fetch/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	smocks "github.com/spacemeshos/go-spacemesh/p2p/server/mocks"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type testFetch struct {
	*Fetch
	mh     *mocks.Mockhost
	mAtxS  *smocks.MockRequestor
	mLyrS  *smocks.MockRequestor
	mOpnS  *smocks.MockRequestor
	mHashS *smocks.MockRequestor
}

func createFetch(tb testing.TB) *testFetch {
	ctrl := gomock.NewController(tb)
	mh := mocks.NewMockhost(ctrl)
	msAtx := smocks.NewMockRequestor(ctrl)
	msLyr := smocks.NewMockRequestor(ctrl)
	msOpn := smocks.NewMockRequestor(ctrl)
	msHash := smocks.NewMockRequestor(ctrl)
	cfg := Config{
		2000, // make sure we never hit the batch timeout
		3,
		3,
		3,
		3,
	}
	return &testFetch{
		Fetch:  newFetch(cfg, mh, datastore.NewBlobStore(sql.InMemory()), msAtx, msLyr, msOpn, msHash, logtest.New(tb)),
		mh:     mh,
		mAtxS:  msAtx,
		mLyrS:  msLyr,
		mOpnS:  msOpn,
		mHashS: msHash,
	}
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
				returnChan:           make(chan ftypes.HashDataPromiseResult, 3),
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
	for _, ch := range []chan ftypes.HashDataPromiseResult{r1, r2, r3} {
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

func TestFetch_GetLayerData(t *testing.T) {
	peers := []p2p.Peer{"p0", "p1", "p3", "p4"}
	errUnknown := errors.New("unknown")
	tt := []struct {
		name string
		errs []error
	}{
		{
			name: "all peers returns",
			errs: []error{nil, nil, nil, nil},
		},
		{
			name: "some peers errors",
			errs: []error{nil, errUnknown, nil, errUnknown},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, len(peers), len(tc.errs))
			f := createFetch(t)
			f.mh.EXPECT().GetPeers().Return(peers)
			oks := make(chan struct{}, len(peers))
			errs := make(chan struct{}, len(peers))
			var wg sync.WaitGroup
			wg.Add(len(peers))
			okFunc := func(data []byte, peer p2p.Peer, numPeers int) {
				require.Equal(t, len(peers), numPeers)
				oks <- struct{}{}
				wg.Done()
			}
			errFunc := func(err error, peer p2p.Peer, numPeers int) {
				require.Equal(t, len(peers), numPeers)
				errs <- struct{}{}
				wg.Done()
			}
			var expOk, expErr int
			for i, p := range peers {
				if tc.errs[i] == nil {
					expOk++
				} else {
					expErr++
				}
				idx := i
				f.mLyrS.EXPECT().Request(gomock.Any(), p, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ p2p.Peer, _ []byte, okFunc func([]byte), errFunc func(error)) error {
						if tc.errs[idx] == nil {
							go okFunc(generateLayerContent(t))
						} else {
							go errFunc(tc.errs[idx])
						}
						return nil
					})
			}
			require.NoError(t, f.GetLayerData(context.TODO(), types.NewLayerID(111), okFunc, errFunc))
			wg.Wait()
			require.Len(t, oks, expOk)
			require.Len(t, errs, expErr)
		})
	}
}

func TestFetch_GetLayerOpinions(t *testing.T) {
	peers := []p2p.Peer{"p0", "p1", "p3", "p4"}
	errUnknown := errors.New("unknown")
	tt := []struct {
		name string
		errs []error
	}{
		{
			name: "all peers returns",
			errs: []error{nil, nil, nil, nil},
		},
		{
			name: "some peers errors",
			errs: []error{nil, errUnknown, nil, errUnknown},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, len(peers), len(tc.errs))
			f := createFetch(t)
			f.mh.EXPECT().GetPeers().Return(peers)
			oks := make(chan struct{}, len(peers))
			errs := make(chan struct{}, len(peers))
			var wg sync.WaitGroup
			wg.Add(len(peers))
			okFunc := func(data []byte, peer p2p.Peer, numPeers int) {
				require.Equal(t, len(peers), numPeers)
				oks <- struct{}{}
				wg.Done()
			}
			errFunc := func(err error, peer p2p.Peer, numPeers int) {
				require.Equal(t, len(peers), numPeers)
				errs <- struct{}{}
				wg.Done()
			}
			var expOk, expErr int
			for i, p := range peers {
				if tc.errs[i] == nil {
					expOk++
				} else {
					expErr++
				}
				idx := i
				f.mOpnS.EXPECT().Request(gomock.Any(), p, gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ p2p.Peer, _ []byte, okFunc func([]byte), errFunc func(error)) error {
						if tc.errs[idx] == nil {
							go okFunc([]byte("data"))
						} else {
							go errFunc(tc.errs[idx])
						}
						return nil
					})
			}
			require.NoError(t, f.GetLayerOpinions(context.TODO(), types.NewLayerID(111), okFunc, errFunc))
			wg.Wait()
			require.Len(t, oks, expOk)
			require.Len(t, errs, expErr)
		})
	}
}

func TestFetch_GetEpochATXIDs(t *testing.T) {
	peers := []p2p.Peer{"p0"}
	errUnknown := errors.New("unknown")
	tt := []struct {
		name string
		err  error
	}{
		{
			name: "success",
		},
		{
			name: "fail",
			err:  errUnknown,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			f.mh.EXPECT().GetPeers().Return(peers)
			var wg sync.WaitGroup
			wg.Add(1)
			okFunc := func(_ []byte, p p2p.Peer) {
				require.Equal(t, peers[0], p)
				wg.Done()
			}
			errFunc := func(err error) {
				require.ErrorIs(t, err, errUnknown)
				wg.Done()
			}
			f.mAtxS.EXPECT().Request(gomock.Any(), peers[0], gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, p p2p.Peer, req []byte, okFunc func([]byte), _ func(error)) error {
					if tc.err == nil {
						go okFunc([]byte("a"))
					} else {
						go errFunc(tc.err)
					}
					return nil
				})
			require.NoError(t, f.GetEpochATXIDs(context.TODO(), types.EpochID(111), okFunc, errFunc))
			wg.Wait()
		})
	}
}
