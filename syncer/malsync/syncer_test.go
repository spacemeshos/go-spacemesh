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
	tb           testing.TB
	syncer       *Syncer
	localdb      *localsql.Database
	db           *sql.Database
	cfg          Config
	ctrl         *gomock.Controller
	fetcher      *mocks.Mockfetcher
	clock        clockwork.FakeClock
	received     map[types.NodeID]bool
	attempts     map[types.NodeID]int
	peers        []p2p.Peer
	peerErrCount *fakeCounter
}

func newTester(tb testing.TB, cfg Config) *tester {
	localdb := localsql.InMemory()
	db := sql.InMemory()
	ctrl := gomock.NewController(tb)
	fetcher := mocks.NewMockfetcher(ctrl)
	clock := clockwork.NewFakeClock()
	peerErrCount := &fakeCounter{}
	syncer := New(fetcher, db, localdb,
		withClock(clock),
		WithConfig(cfg),
		WithLogger(zaptest.NewLogger(tb)),
		WithPeerErrMetric(peerErrCount),
	)
	return &tester{
		tb:           tb,
		syncer:       syncer,
		localdb:      localdb,
		db:           db,
		cfg:          cfg,
		ctrl:         ctrl,
		fetcher:      fetcher,
		clock:        clock,
		received:     make(map[types.NodeID]bool),
		attempts:     make(map[types.NodeID]int),
		peers:        []p2p.Peer{"a", "b", "c"},
		peerErrCount: peerErrCount,
	}
}

func (tester *tester) expectGetMaliciousIDs() {
	// "2" comes just from a single peer
	tester.fetcher.EXPECT().
		GetMaliciousIDs(gomock.Any(), tester.peers[0]).
		Return(malData("4", "1", "3", "2"), nil)
	for _, p := range tester.peers[1:] {
		tester.fetcher.EXPECT().
			GetMaliciousIDs(gomock.Any(), p).
			Return(malData("4", "1", "3"), nil)
	}
}

func (tester *tester) expectGetProofs(errMap map[types.NodeID]error) {
	tester.fetcher.EXPECT().
		GetMalfeasanceProofs(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, ids []types.NodeID) error {
			batchErr := &fetch.BatchError{
				Errors: make(map[types.Hash32]error),
			}
			for _, id := range ids {
				tester.attempts[id]++
				require.NotContains(tester.tb, tester.received, id)
				if err := errMap[id]; err != nil {
					batchErr.Errors[types.Hash32(id)] = err
					continue
				}
				tester.received[id] = true
				proofData := codec.MustEncode(mproof(id))
				require.NoError(tester.tb, identities.SetMalicious(
					tester.db, id, proofData, tester.syncer.clock.Now()))
			}
			if len(batchErr.Errors) != 0 {
				return batchErr
			}
			return nil
		}).AnyTimes()
}

func (tester *tester) expectPeers(peers []p2p.Peer) {
	tester.fetcher.EXPECT().SelectBestShuffled(tester.cfg.MalfeasanceIDPeers).Return(peers).AnyTimes()
}

func TestSyncer(t *testing.T) {
	t.Run("EnsureInSync", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		tester.expectGetMaliciousIDs()
		tester.expectGetProofs(nil)
		epochStart := tester.clock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("2"), nid("3"), nid("4"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): 1,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attempts)
		tester.clock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.Zero(t, tester.peerErrCount.n)
	})
	t.Run("EnsureInSync with no malfeasant identities", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		for _, p := range tester.peers {
			tester.fetcher.EXPECT().
				GetMaliciousIDs(gomock.Any(), p).
				Return(nil, nil)
		}
		epochStart := tester.clock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.Zero(t, tester.peerErrCount.n)
	})
	t.Run("interruptible", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		tester.expectPeers([]p2p.Peer{"a"})
		tester.fetcher.EXPECT().
			GetMaliciousIDs(gomock.Any(), gomock.Any()).
			Return(malData("1"), nil).AnyTimes()
		tester.fetcher.EXPECT().
			GetMalfeasanceProofs(gomock.Any(), gomock.Any()).
			Return(errors.New("no atxs")).AnyTimes()
		require.ErrorIs(t, tester.syncer.DownloadLoop(ctx), context.Canceled)
	})
	t.Run("retries on no peers", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		ctx, cancel := context.WithCancel(context.Background())
		ch := make(chan []p2p.Peer)
		tester.fetcher.EXPECT().SelectBestShuffled(tester.cfg.MalfeasanceIDPeers).
			DoAndReturn(func(int) []p2p.Peer {
				return <-ch
			}).AnyTimes()
		var eg errgroup.Group
		eg.Go(func() error {
			require.ErrorIs(t, tester.syncer.DownloadLoop(ctx), context.Canceled)
			return nil
		})
		tester.clock.BlockUntil(1)
		tester.clock.Advance(tester.cfg.IDRequestInterval)
		ch <- nil
		tester.clock.BlockUntil(1)
		tester.clock.Advance(tester.cfg.IDRequestInterval)

		tester.expectGetMaliciousIDs()
		tester.expectGetProofs(nil)
		ch <- tester.peers
		tester.clock.BlockUntil(1)
		cancel()
		eg.Wait()
	})
	t.Run("gettings ids from MinSyncPeers peers is enough", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.MinSyncPeers = 2
		tester := newTester(t, cfg)
		tester.expectPeers(tester.peers)
		tester.fetcher.EXPECT().
			GetMaliciousIDs(gomock.Any(), tester.peers[0]).
			Return(nil, errors.New("fail"))
		for _, p := range tester.peers[1:] {
			tester.fetcher.EXPECT().
				GetMaliciousIDs(gomock.Any(), p).
				Return(malData("4", "1", "3", "2"), nil)
		}
		tester.expectGetProofs(nil)
		epochStart := tester.clock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("2"), nid("3"), nid("4"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): 1,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attempts)
		tester.clock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.Equal(t, 1, tester.peerErrCount.n)
	})
	t.Run("skip hashes after max retries", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.RequestsLimit = 3
		tester := newTester(t, cfg)
		tester.expectPeers(tester.peers)
		tester.expectGetMaliciousIDs()
		tester.expectGetProofs(map[types.NodeID]error{
			nid("2"): errors.New("fail"),
		})
		epochStart := tester.clock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("3"), nid("4"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): tester.cfg.RequestsLimit,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attempts)
		tester.clock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
	})
	t.Run("skip hashes after validation reject", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		tester.expectPeers(tester.peers)
		tester.expectGetMaliciousIDs()
		tester.expectGetProofs(map[types.NodeID]error{
			// note that "2" comes just from a single peer
			// (see expectGetMaliciousIDs)
			nid("2"): pubsub.ErrValidationReject,
		})
		epochStart := tester.clock.Now().Truncate(time.Second)
		epochEnd := epochStart.Add(10 * time.Minute)
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
		require.ElementsMatch(t, []types.NodeID{
			nid("1"), nid("3"), nid("4"),
		}, maps.Keys(tester.received))
		require.Equal(t, map[types.NodeID]int{
			nid("1"): 1,
			nid("2"): 1,
			nid("3"): 1,
			nid("4"): 1,
		}, tester.attempts)
		tester.clock.Advance(1 * time.Minute)
		// second call does nothing after recent sync
		require.NoError(t,
			tester.syncer.EnsureInSync(context.Background(), epochStart, epochEnd))
	})
}
