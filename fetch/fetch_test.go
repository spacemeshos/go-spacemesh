package fetch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/proposals/store"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type testFetch struct {
	*Fetch
	mh      *mocks.Mockhost
	mMalS   *mocks.Mockrequester
	mAtxS   *mocks.Mockrequester
	mLyrS   *mocks.Mockrequester
	mHashS  *mocks.Mockrequester
	mMHashS *mocks.Mockrequester
	mOpn2S  *mocks.Mockrequester

	mMalH        *mocks.MockSyncValidator
	mAtxH        *mocks.MockSyncValidator
	mBallotH     *mocks.MockSyncValidator
	mActiveSetH  *mocks.MockSyncValidator
	mBlocksH     *mocks.MockSyncValidator
	mProposalH   *mocks.MockSyncValidator
	method       int
	mTxBlocksH   *mocks.MockSyncValidator
	mTxProposalH *mocks.MockSyncValidator
	mPoetH       *mocks.MockSyncValidator
}

func createFetch(tb testing.TB) *testFetch {
	ctrl := gomock.NewController(tb)
	tf := &testFetch{
		mh:           mocks.NewMockhost(ctrl),
		mMalS:        mocks.NewMockrequester(ctrl),
		mAtxS:        mocks.NewMockrequester(ctrl),
		mLyrS:        mocks.NewMockrequester(ctrl),
		mHashS:       mocks.NewMockrequester(ctrl),
		mMHashS:      mocks.NewMockrequester(ctrl),
		mOpn2S:       mocks.NewMockrequester(ctrl),
		mMalH:        mocks.NewMockSyncValidator(ctrl),
		mAtxH:        mocks.NewMockSyncValidator(ctrl),
		mBallotH:     mocks.NewMockSyncValidator(ctrl),
		mActiveSetH:  mocks.NewMockSyncValidator(ctrl),
		mBlocksH:     mocks.NewMockSyncValidator(ctrl),
		mProposalH:   mocks.NewMockSyncValidator(ctrl),
		mTxBlocksH:   mocks.NewMockSyncValidator(ctrl),
		mTxProposalH: mocks.NewMockSyncValidator(ctrl),
		mPoetH:       mocks.NewMockSyncValidator(ctrl),
	}
	for _, srv := range []*mocks.Mockrequester{tf.mMalS, tf.mAtxS, tf.mLyrS, tf.mHashS, tf.mMHashS, tf.mOpn2S} {
		srv.EXPECT().Run(gomock.Any()).AnyTimes()
	}
	cfg := Config{
		BatchTimeout:         2 * time.Second, // make sure we never hit the batch timeout
		BatchSize:            3,
		QueueSize:            1000,
		RequestTimeout:       3 * time.Second,
		RequestHardTimeout:   10 * time.Second,
		MaxRetriesForRequest: 3,
		GetAtxsConcurrency:   DefaultConfig().GetAtxsConcurrency,
	}
	lg := logtest.New(tb)

	tf.Fetch = NewFetch(datastore.NewCachedDB(sql.InMemory(), lg), store.New(), nil,
		WithContext(context.TODO()),
		WithConfig(cfg),
		WithLogger(lg),
		withServers(map[string]requester{
			malProtocol:      tf.mMalS,
			atxProtocol:      tf.mAtxS,
			lyrDataProtocol:  tf.mLyrS,
			hashProtocol:     tf.mHashS,
			meshHashProtocol: tf.mMHashS,
			OpnProtocol:      tf.mOpn2S,
		}),
		withHost(tf.mh))
	tf.Fetch.SetValidators(
		tf.mAtxH,
		tf.mPoetH,
		tf.mBallotH,
		tf.mActiveSetH,
		tf.mBlocksH,
		tf.mProposalH,
		tf.mTxBlocksH,
		tf.mTxProposalH,
		tf.mMalH,
	)
	return tf
}

func goodReceiver(context.Context, types.Hash32, p2p.Peer, []byte) error {
	return nil
}

func badReceiver(context.Context, types.Hash32, p2p.Peer, []byte) error {
	return errors.New("bad receiver")
}

func TestFetch_Start(t *testing.T) {
	lg := logtest.New(t)
	f := NewFetch(datastore.NewCachedDB(sql.InMemory(), lg), store.New(), nil,
		WithContext(context.TODO()),
		WithConfig(DefaultConfig()),
		WithLogger(lg),
		withServers(map[string]requester{
			malProtocol: nil,
		}),
	)
	require.ErrorIs(t, f.Start(), errValidatorsNotSet)
}

func TestFetch_GetHash(t *testing.T) {
	f := createFetch(t)
	require.NoError(t, f.Start())
	defer f.Stop()
	h1 := types.RandomHash()
	hint := datastore.POETDB
	hint2 := datastore.BallotDB

	// test hash aggregation
	p0, err := f.getHash(context.TODO(), h1, hint, goodReceiver)
	require.NoError(t, err)
	p1, err := f.getHash(context.TODO(), h1, hint, goodReceiver)
	require.NoError(t, err)
	require.Equal(t, p0.completed, p1.completed)

	h2 := types.RandomHash()
	p2, err := f.getHash(context.TODO(), h2, hint2, goodReceiver)
	require.NoError(t, err)
	require.NotEqual(t, p1.completed, p2.completed)
}

func TestFetch_RequestHashBatchFromPeers(t *testing.T) {
	tt := []struct {
		name       string
		nErr, vErr error
	}{
		{
			name: "request batch",
		},
		{
			name: "request batch network failure",
			nErr: errors.New("network failure"),
		},
		{
			name: "request batch validation failure",
			vErr: errors.New("validation failure"),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := createFetch(t)
			f.cfg.MaxRetriesForRequest = 0
			peer := p2p.Peer("buddy")
			f.peers.Add(peer)

			hsh0 := types.RandomHash()
			res0 := ResponseMessage{
				Hash: hsh0,
				Data: []byte("a"),
			}
			hsh1 := types.RandomHash()
			res1 := ResponseMessage{
				Hash: hsh1,
				Data: []byte("b"),
			}
			f.mHashS.EXPECT().
				Request(gomock.Any(), peer, gomock.Any()).
				DoAndReturn(func(_ context.Context, _ p2p.Peer, req []byte, extraProtocols ...string) ([]byte, error) {
					if tc.nErr != nil {
						return nil, tc.nErr
					}
					var rb RequestBatch
					err := codec.Decode(req, &rb)
					require.NoError(t, err)
					resBatch := ResponseBatch{
						ID:        rb.ID,
						Responses: []ResponseMessage{res0, res1},
					}
					bts, err := codec.Encode(&resBatch)
					require.NoError(t, err)
					return bts, nil
				})

			var p0, p1 []*promise
			// query each hash twice
			receiver := goodReceiver
			if tc.vErr != nil {
				receiver = badReceiver
			}
			for i := 0; i < 2; i++ {
				p, err := f.getHash(context.TODO(), hsh0, datastore.ProposalDB, receiver)
				require.NoError(t, err)
				p0 = append(p0, p)
				p, err = f.getHash(context.TODO(), hsh1, datastore.BlockDB, receiver)
				require.NoError(t, err)
				p1 = append(p1, p)
			}
			require.Equal(t, p0[0], p0[1])
			require.Equal(t, p1[0], p1[1])

			f.requestHashBatchFromPeers()

			for _, p := range append(p0, p1...) {
				<-p.completed
				if tc.nErr != nil || tc.vErr != nil {
					require.Error(t, p.err)
				} else {
					require.NoError(t, p.err)
				}
			}
		})
	}
}

func TestFetch_GetHash_StartStopSanity(t *testing.T) {
	f := createFetch(t)
	require.NoError(t, f.Start())
	f.Stop()
}

func TestFetch_Loop_BatchRequestMax(t *testing.T) {
	f := createFetch(t)
	f.cfg.BatchTimeout = 1
	f.cfg.BatchSize = 2
	peer := p2p.Peer("buddy")
	f.peers.Add(peer)

	h1 := types.RandomHash()
	h2 := types.RandomHash()
	h3 := types.RandomHash()
	f.mHashS.EXPECT().
		Request(gomock.Any(), peer, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ p2p.Peer, req []byte, extraProtocols ...string) ([]byte, error) {
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
			return bts, nil
		}).
		Times(2)
	// 3 requests with batch size 2 -> 2 sends

	hint := datastore.POETDB

	defer f.Stop()
	require.NoError(t, f.Start())
	p1, err := f.getHash(context.TODO(), h1, hint, goodReceiver)
	require.NoError(t, err)
	p2, err := f.getHash(context.TODO(), h2, hint, goodReceiver)
	require.NoError(t, err)
	p3, err := f.getHash(context.TODO(), h3, hint, goodReceiver)
	require.NoError(t, err)
	for _, p := range []*promise{p1, p2, p3} {
		<-p.completed
		require.NoError(t, p.err)
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
	require.False(t, allTheSame)
}

func TestFetch_RegisterPeerHashes(t *testing.T) {
	myPeers := make([]p2p.Peer, 10)
	for i := 0; i < len(myPeers); i++ {
		myPeers[i] = p2p.Peer(types.RandomBytes(20))
	}
	f := createFetch(t)
	hostID := p2p.Peer("self")
	f.mh.EXPECT().ID().Return(hostID).AnyTimes()
	hashes := []types.Hash32{{1, 2, 3}, {4, 5, 6}}
	f.RegisterPeerHashes(hostID, hashes)
	require.Zero(t, f.hashToPeers.Len())

	peer := p2p.Peer("buddy")
	f.RegisterPeerHashes(peer, hashes)
	require.Equal(t, len(hashes), f.hashToPeers.Len())
}

func TestFetch_PeerDroppedWhenMessageResultsInValidationReject(t *testing.T) {
	lg := logtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	cfg := Config{
		BatchTimeout:         2000 * time.Minute, // make sure we never hit the batch timeout
		BatchSize:            3,
		QueueSize:            1000,
		RequestTimeout:       3 * time.Second,
		RequestHardTimeout:   10 * time.Second,
		MaxRetriesForRequest: 3,
	}
	p2pconf := p2p.DefaultConfig()
	p2pconf.Listen = p2p.MustParseAddresses("/ip4/127.0.0.1/tcp/0")
	p2pconf.DataDir = t.TempDir()
	p2pconf.IP4Blocklist = nil

	// Good host
	h, err := p2p.New(ctx, lg, p2pconf, []byte{}, []byte{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, h.Stop()) })

	// Bad host, will send a message that results in validation reject
	p2pconf.DataDir = t.TempDir()
	badPeerHost, err := p2p.New(ctx, lg, p2pconf, []byte{}, []byte{})
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, badPeerHost.Stop()) })

	err = h.Connect(ctx, peer.AddrInfo{
		ID:    badPeerHost.ID(),
		Addrs: badPeerHost.Addrs(),
	})
	require.NoError(t, err)

	require.Equal(t, 1, len(h.GetPeers()))

	// This handler returns a ResponseBatch with an empty response that will fail validation on the remote peer
	badPeerHandler := func(_ context.Context, data []byte) ([]byte, error) {
		var b RequestBatch
		codec.Decode(data, &b)

		r := ResponseBatch{
			ID:        b.ID,
			Responses: []ResponseMessage{{}},
		}
		result, err := codec.Encode(&r)
		// This runs in a different goroutine so we can't call t.Fatal or equivalent
		if err != nil {
			panic(err.Error())
		}
		return result, nil
	}
	badsrv := server.New(badPeerHost, hashProtocol, server.WrapHandler(badPeerHandler))
	var eg errgroup.Group
	eg.Go(func() error {
		badsrv.Run(ctx)
		return nil
	})
	defer eg.Wait()

	fetcher := NewFetch(datastore.NewCachedDB(sql.InMemory(), lg), store.New(), h,
		WithContext(ctx),
		WithConfig(cfg),
		WithLogger(lg),
	)
	t.Cleanup(fetcher.Stop)

	// We set a validatior just for atxs, this validator does not drop connections
	vf := ValidatorFunc(
		func(context.Context, types.Hash32, peer.ID, []byte) error { return pubsub.ErrValidationReject },
	)
	fetcher.SetValidators(vf, nil, nil, nil, nil, nil, nil, nil, nil)

	// Request an atx by hash
	_, err = fetcher.getHash(
		ctx,
		types.Hash32{},
		datastore.ATXDB,
		fetcher.validators.atx.HandleMessage,
	)
	require.NoError(t, err)
	fetcher.requestHashBatchFromPeers()

	// Verify that connections remain up
	for i := 0; i < 5; i++ {
		conns := h.Network().ConnsToPeer(badPeerHost.ID())
		require.Equal(t, 1, len(conns))
		time.Sleep(100 * time.Millisecond)
	}

	// Now wrap the atx validator with  DropPeerOnValidationReject and set it again
	fetcher.SetValidators(
		ValidatorFunc(pubsub.DropPeerOnSyncValidationReject(vf, h, lg)),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	// Request an atx by hash
	_, err = fetcher.getHash(
		ctx,
		types.Hash32{},
		datastore.ATXDB,
		fetcher.validators.atx.HandleMessage,
	)
	require.NoError(t, err)
	fetcher.requestHashBatchFromPeers()

	// See that the connection gets dropped
	require.Eventually(t, func() bool {
		return len(h.Host.Network().ConnsToPeer(badPeerHost.ID())) == 0
	}, time.Second*15, time.Millisecond*200)
	require.Equal(t, 0, len(h.GetPeers()))
}
