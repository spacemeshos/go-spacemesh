package malsync

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/jonboulle/clockwork"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/exp/maps"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/sql/malfeasance"
	"github.com/spacemeshos/go-spacemesh/sql/statesql"
	"github.com/spacemeshos/go-spacemesh/syncer/malsync/mocks"
)

type fakeCounter struct {
	n int
}

func (fc *fakeCounter) Inc() { fc.n++ }

func genNodeIDs(n int) []types.NodeID {
	ids := make([]types.NodeID, n)
	for i := range ids {
		ids[i] = types.RandomNodeID()
	}
	return ids
}

func TestSyncState(t *testing.T) {
	nodeIDs := genNodeIDs(5)
	sst := newSyncState(3, true)
	require.Zero(t, sst.numSyncedPeers())
	require.False(t, sst.has(nodeIDs[0]))
	sst.update(malUpdate{
		peer:    "a",
		nodeIDs: slices.Clone(nodeIDs[:4]),
	})
	for _, id := range nodeIDs[:4] {
		require.True(t, sst.has(id))
	}
	require.False(t, sst.has(nodeIDs[4]))
	ids, err := sst.missing(10, func(nodeID types.NodeID) (bool, error) { return false, nil })
	require.NoError(t, err)
	require.ElementsMatch(t, nodeIDs[:4], ids)

	testErr := errors.New("fail")
	_, err = sst.missing(10, func(nodeID types.NodeID) (bool, error) { return false, testErr })
	require.ErrorIs(t, err, testErr)

	sst.downloaded(nodeIDs[0])
	sst.failed(nodeIDs[1])
	sst.rejected(nodeIDs[2])

	ids, err = sst.missing(10, func(nodeID types.NodeID) (bool, error) { return false, nil })
	require.NoError(t, err)
	require.ElementsMatch(t, []types.NodeID{nodeIDs[1], nodeIDs[3]}, ids)

	// make nodeIDs[1] fail too many times
	sst.failed(nodeIDs[1])
	sst.failed(nodeIDs[1])

	ids, err = sst.missing(10, func(nodeID types.NodeID) (bool, error) { return false, nil })
	require.NoError(t, err)
	require.ElementsMatch(t, []types.NodeID{nodeIDs[3]}, ids)

	for i := 0; i < 2; i++ {
		ids, err = sst.missing(10, func(nodeID types.NodeID) (bool, error) {
			// nodeIDs[3] will be marked as downloaded
			return nodeID == nodeIDs[3], nil
		})
		require.NoError(t, err)
		require.Empty(t, ids)
	}

	require.Zero(t, sst.numSyncedPeers())
	sst.done()
	require.Equal(t, 1, sst.numSyncedPeers())

	sst.update(malUpdate{
		peer: "b",
	})
	require.Equal(t, 1, sst.numSyncedPeers())
	sst.done()
	require.Equal(t, 2, sst.numSyncedPeers())
}

func mproof(nodeID types.NodeID) *wire.MalfeasanceProof {
	var ballotProof wire.BallotProof
	for i := 0; i < 2; i++ {
		ballotProof.Messages[i] = wire.BallotProofMsg{
			InnerMsg: types.BallotMetadata{
				Layer:   types.LayerID(9),
				MsgHash: types.RandomHash(),
			},
			Signature: types.RandomEdSignature(),
			SmesherID: nodeID,
		}
	}

	return &wire.MalfeasanceProof{
		Layer: types.LayerID(11),
		Proof: wire.Proof{
			Type: wire.MultipleBallots,
			Data: &ballotProof,
		},
	}
}

func nid(id string) types.NodeID {
	var nodeID types.NodeID
	copy(nodeID[:], id)
	return nodeID
}

func malData(ids ...string) []types.NodeID {
	malIDs := make([]types.NodeID, len(ids))
	for n, id := range ids {
		malIDs[n] = nid(id)
	}
	return malIDs
}

type tester struct {
	tb       testing.TB
	syncer   *Syncer
	db       sql.StateDatabase
	cfg      Config
	mFetcher *mocks.Mockfetcher
	mTicker  *mocks.MocklayerClock
	mClock   *clockwork.FakeClock

	peers          []p2p.Peer
	peerErrCount   *fakeCounter
	receivedLegacy map[types.NodeID]bool
	attemptsLegacy map[types.NodeID]int
	received       map[types.NodeID]bool
	attempts       map[types.NodeID]int
}

func newTester(tb testing.TB, cfg Config) *tester {
	localDB := localsql.InMemoryTest(tb)
	db := statesql.InMemoryTest(tb)
	ctrl := gomock.NewController(tb)
	fetcher := mocks.NewMockfetcher(ctrl)
	ticker := mocks.NewMocklayerClock(ctrl)
	clock := clockwork.NewFakeClock()
	peerErrCount := &fakeCounter{}
	syncer := New(fetcher, db, localDB, ticker,
		WithConfig(cfg),
		WithLogger(zaptest.NewLogger(tb)),
		WithPeerErrMetric(peerErrCount),
		withClock(clock),
	)
	return &tester{
		tb:             tb,
		syncer:         syncer,
		db:             db,
		cfg:            cfg,
		mFetcher:       fetcher,
		mTicker:        ticker,
		mClock:         clock,
		receivedLegacy: make(map[types.NodeID]bool),
		attemptsLegacy: make(map[types.NodeID]int),
		received:       make(map[types.NodeID]bool),
		attempts:       make(map[types.NodeID]int),
		peers:          []p2p.Peer{"a", "b", "c"},
		peerErrCount:   peerErrCount,
	}
}

func (tester *tester) expectLegacyMaliciousIDs() {
	// "2" comes just from a single peer via legacy protocol
	tester.mFetcher.EXPECT().
		LegacyMaliciousIDs(gomock.Any(), tester.peers[0]).
		Return(malData("4", "1", "3", "2"), nil)
	for _, p := range tester.peers[1:] {
		tester.mFetcher.EXPECT().
			LegacyMaliciousIDs(gomock.Any(), p).
			Return(malData("4", "1", "3"), nil)
	}
}

func (tester *tester) expectMaliciousIDs() {
	// "102" comes just from a single peer
	tester.mFetcher.EXPECT().
		MaliciousIDs(gomock.Any(), tester.peers[0]).
		Return(malData("104", "101", "103", "102"), nil)
	for _, p := range tester.peers[1:] {
		tester.mFetcher.EXPECT().
			MaliciousIDs(gomock.Any(), p).
			Return(malData("104", "101", "103"), nil)
	}
}

func (t *tester) expectLegacyProofs(errMap map[types.NodeID]error) {
	t.mFetcher.EXPECT().
		LegacyMalfeasanceProofs(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, ids []types.NodeID) error {
			batchErr := &fetch.BatchError{
				Errors: make(map[types.Hash32]error),
			}
			for _, id := range ids {
				t.attemptsLegacy[id]++
				require.NotContains(t.tb, t.receivedLegacy, id)
				if err := errMap[id]; err != nil {
					batchErr.Errors[types.Hash32(id)] = err
					continue
				}
				t.receivedLegacy[id] = true
				proofData := codec.MustEncode(mproof(id))
				require.NoError(t.tb, identities.SetMalicious(t.db, id, proofData, t.syncer.clock.Now()))
			}
			if len(batchErr.Errors) != 0 {
				return batchErr
			}
			return nil
		}).AnyTimes()
}

func (t *tester) expectProofs(errMap map[types.NodeID]error) {
	t.mFetcher.EXPECT().
		MalfeasanceProofs(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, ids []types.NodeID) error {
			batchErr := &fetch.BatchError{
				Errors: make(map[types.Hash32]error),
			}
			for _, id := range ids {
				t.attempts[id]++
				require.NotContains(t.tb, t.received, id)
				if err := errMap[id]; err != nil {
					batchErr.Errors[types.Hash32(id)] = err
					continue
				}
				t.received[id] = true
				proof := codec.MustEncode(mproof(id))
				require.NoError(t.tb, malfeasance.AddProof(t.db, id, nil, proof, 1, t.syncer.clock.Now()))
			}
			if len(batchErr.Errors) != 0 {
				return batchErr
			}
			return nil
		}).AnyTimes()
}

func (tester *tester) expectPeers(peers []p2p.Peer) {
	tester.mFetcher.EXPECT().SelectBestShuffled(tester.cfg.MalfeasanceIDPeers).Return(peers).AnyTimes()
}

func TestSyncer(t *testing.T) {
	t.Run("EnsureLegacyInSync", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		tester.expectLegacyMaliciousIDs()
		tester.expectLegacyProofs(nil)
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("2"), nid("3"), nid("4"),
		}, maps.Keys(tester.receivedLegacy))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): 1,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attemptsLegacy)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
		require.Zero(t, tester.peerErrCount.n)
	})
	t.Run("EnsureInSync", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		tester.expectMaliciousIDs()
		tester.expectProofs(nil)
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("101"), nid("102"), nid("103"), nid("104"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("101"): 1,
			nid("102"): 1,
			nid("103"): 1,
			nid("104"): 1,
		}, tester.attempts)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
	})
	t.Run("EnsureLegacyInSync with no malfeasant identities", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		for _, p := range tester.peers {
			tester.mFetcher.EXPECT().
				LegacyMaliciousIDs(gomock.Any(), p).
				Return(nil, nil)
		}
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
		require.Zero(t, tester.peerErrCount.n)
	})
	t.Run("EnsureInSync with no malfeasant identities", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		for _, p := range tester.peers {
			tester.mFetcher.EXPECT().
				MaliciousIDs(gomock.Any(), p).
				Return(nil, nil)
		}
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.Zero(t, tester.peerErrCount.n)
	})
	t.Run("interruptible", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		tester.expectPeers([]p2p.Peer{"a"})
		tester.mFetcher.EXPECT().
			LegacyMaliciousIDs(gomock.Any(), gomock.Any()).
			Return(malData("1"), nil).AnyTimes()
		tester.mFetcher.EXPECT().
			LegacyMalfeasanceProofs(gomock.Any(), gomock.Any()).
			Return(errors.New("no atxs")).AnyTimes()
		tester.mFetcher.EXPECT().
			MaliciousIDs(gomock.Any(), gomock.Any()).
			Return(malData("101"), nil).AnyTimes()
		tester.mFetcher.EXPECT().
			MalfeasanceProofs(gomock.Any(), gomock.Any()).
			Return(errors.New("no atxs")).AnyTimes()
		tester.mTicker.EXPECT().AwaitLayer(types.EpochID(10).FirstLayer()).DoAndReturn(
			func(_ types.LayerID) <-chan struct{} {
				ch := make(chan struct{})
				close(ch)
				return ch
			},
		)
		require.ErrorIs(t, tester.syncer.DownloadLoop(ctx, types.EpochID(10)), context.Canceled)
	})
	t.Run("retries on no peers", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan []p2p.Peer)
		tester.mFetcher.EXPECT().SelectBestShuffled(tester.cfg.MalfeasanceIDPeers).
			DoAndReturn(func(int) []p2p.Peer {
				return <-ch
			}).AnyTimes()
		tester.mTicker.EXPECT().AwaitLayer(types.EpochID(10).FirstLayer()).DoAndReturn(
			func(_ types.LayerID) <-chan struct{} {
				ch := make(chan struct{})
				close(ch)
				return ch
			},
		)
		var eg errgroup.Group
		eg.Go(func() error {
			require.ErrorIs(t, tester.syncer.DownloadLoop(ctx, types.EpochID(10)), context.Canceled)
			return nil
		})
		tester.mClock.BlockUntilContext(context.Background(), 2)
		tester.mClock.Advance(tester.cfg.IDRequestInterval)
		ch <- nil
		ch <- nil
		tester.mClock.BlockUntilContext(context.Background(), 2)
		tester.mClock.Advance(tester.cfg.IDRequestInterval)

		tester.expectLegacyMaliciousIDs()
		tester.expectLegacyProofs(nil)
		tester.expectMaliciousIDs()
		tester.expectProofs(nil)
		ch <- tester.peers
		ch <- tester.peers
		tester.mClock.BlockUntilContext(context.Background(), 2)
		cancel()
		eg.Wait()
	})
	t.Run("getting ids from MinSyncPeers peers is enough - legacy", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MinSyncPeers = 2
		tester := newTester(t, cfg)
		tester.expectPeers(tester.peers)
		tester.mFetcher.EXPECT().
			LegacyMaliciousIDs(gomock.Any(), tester.peers[0]).
			Return(nil, errors.New("fail"))
		for _, p := range tester.peers[1:] {
			tester.mFetcher.EXPECT().
				LegacyMaliciousIDs(gomock.Any(), p).
				Return(malData("4", "1", "3", "2"), nil)
		}
		tester.expectLegacyProofs(nil)
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t,
			tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("2"), nid("3"), nid("4"),
		}, maps.Keys(tester.receivedLegacy))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): 1,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attemptsLegacy)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
		require.Equal(t, 1, tester.peerErrCount.n)
	})
	t.Run("getting ids from MinSyncPeers peers is enough", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MinSyncPeers = 2
		tester := newTester(t, cfg)
		tester.expectPeers(tester.peers)
		tester.mFetcher.EXPECT().
			MaliciousIDs(gomock.Any(), tester.peers[0]).
			Return(nil, errors.New("fail"))
		for _, p := range tester.peers[1:] {
			tester.mFetcher.EXPECT().
				MaliciousIDs(gomock.Any(), p).
				Return(malData("104", "101", "103", "102"), nil)
		}
		tester.expectProofs(nil)
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("101"), nid("102"), nid("103"), nid("104"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("101"): 1,
			nid("102"): 1,
			nid("103"): 1,
			nid("104"): 1,
		}, tester.attempts)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.Equal(t, 1, tester.peerErrCount.n)
	})
	t.Run("skip hashes after max retries - legacy", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RequestsLimit = 3
		tester := newTester(t, cfg)
		tester.expectPeers(tester.peers)
		tester.expectLegacyMaliciousIDs()
		tester.expectLegacyProofs(map[types.NodeID]error{
			nid("2"): errors.New("fail"),
		})
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("3"), nid("4"),
		}, maps.Keys(tester.receivedLegacy))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): tester.cfg.RequestsLimit,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attemptsLegacy)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
	})
	t.Run("skip hashes after max retries", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RequestsLimit = 3
		tester := newTester(t, cfg)
		tester.expectPeers(tester.peers)
		tester.expectMaliciousIDs()
		tester.expectProofs(map[types.NodeID]error{
			nid("102"): errors.New("fail"),
		})
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("101"), nid("103"), nid("104"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("101"): 1,
			nid("102"): tester.cfg.RequestsLimit,
			nid("103"): 1,
			nid("104"): 1,
		}, tester.attempts)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
	})
	t.Run("skip hashes after validation reject - legacy", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		tester.expectLegacyMaliciousIDs()
		tester.expectLegacyProofs(map[types.NodeID]error{
			// note that "2" comes just from a single peer
			// (see expectMaliciousIDs)
			nid("2"): pubsub.ErrValidationReject,
		})
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("3"), nid("4"),
		}, maps.Keys(tester.receivedLegacy))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): 1,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attemptsLegacy)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureLegacyInSync(context.Background(), epochStart, epochEnd))
	})
	t.Run("skip hashes after validation reject", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		tester.expectMaliciousIDs()
		tester.expectProofs(map[types.NodeID]error{
			// note that "102" comes just from a single peer
			// (see expectMaliciousIDs)
			nid("102"): pubsub.ErrValidationReject,
		})
		epochStart := tester.mClock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("101"), nid("103"), nid("104"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("101"): 1,
			nid("102"): 1,
			nid("103"): 1,
			nid("104"): 1,
		}, tester.attempts)
		tester.mClock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t, tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
	})
}
