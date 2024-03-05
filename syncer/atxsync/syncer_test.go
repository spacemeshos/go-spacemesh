package atxsync

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/atxsync"
	"github.com/spacemeshos/go-spacemesh/sql/localsql"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync/mocks"
	"github.com/spacemeshos/go-spacemesh/system"
)

func init() {
	types.SetLayersPerEpoch(10)
}

func aid(id string) types.ATXID {
	var atx types.ATXID
	copy(atx[:], id)
	return atx
}

func edata(ids ...string) *fetch.EpochData {
	ed := &fetch.EpochData{}
	for _, id := range ids {
		ed.AtxIDs = append(ed.AtxIDs, aid(id))
	}
	return ed
}

func newTester(tb testing.TB, cfg Config) *tester {
	localdb := localsql.InMemory()
	db := sql.InMemory()
	ctrl := gomock.NewController(tb)
	clock := mocks.NewMockclock(ctrl)
	fetcher := mocks.NewMockfetcher(ctrl)
	syncer := New(fetcher, clock, db, localdb, WithConfig(cfg), WithLogger(logtest.New(tb).Zap()))
	return &tester{
		tb:      tb,
		syncer:  syncer,
		localdb: localdb,
		db:      db,
		cfg:     cfg,
		ctrl:    ctrl,
		clock:   clock,
		fetcher: fetcher,
	}
}

type tester struct {
	tb      testing.TB
	syncer  *Syncer
	localdb *localsql.Database
	db      *sql.Database
	cfg     Config
	ctrl    *gomock.Controller
	clock   *mocks.Mockclock
	fetcher *mocks.Mockfetcher
}

func TestSyncer(t *testing.T) {
	t.Run("sanity", func(t *testing.T) {
		tester := newTester(t, Config{
			EpochInfoInterval: 100 * time.Microsecond,
			EpochInfoPeers:    3,
			RequestsLimit:     10,
			AtxsBatch:         10,
		})

		peers := []p2p.Peer{"a", "b", "c"}
		tester.fetcher.EXPECT().SelectBestShuffled(tester.cfg.EpochInfoPeers).Return(peers).AnyTimes()
		publish := types.EpochID(1)
		for _, p := range peers {
			tester.fetcher.EXPECT().
				PeerEpochInfo(gomock.Any(), p, publish).
				Return(edata("4", "1", "3", "2"), nil).
				AnyTimes()
		}

		tester.fetcher.EXPECT().
			GetAtxs(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, ids []types.ATXID, _ ...system.GetAtxOpt) error {
				for _, id := range ids {
					require.NoError(t, atxs.Add(tester.db, atx(id)))
				}
				return nil
			}).AnyTimes()

		past := time.Now().Add(-time.Minute)
		tester.clock.EXPECT().LayerToTime(publish.FirstLayer()).Return(past).AnyTimes()
		require.NoError(t, tester.syncer.Download(context.Background(), publish))
	})
	t.Run("interruptible", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		publish := types.EpochID(1)
		tester.clock.EXPECT().LayerToTime(publish.FirstLayer()).Return(time.Now()).AnyTimes()
		require.NoError(t, tester.syncer.Download(ctx, publish))
	})
	t.Run("error on no peers", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		publish := types.EpochID(1)
		tester.clock.EXPECT().LayerToTime(publish.FirstLayer()).Return(time.Now()).AnyTimes()
		tester.fetcher.EXPECT().SelectBestShuffled(tester.cfg.EpochInfoPeers).Return(nil)
		require.ErrorContains(t, tester.syncer.Download(context.Background(), publish), "no peers available")
	})
	t.Run("terminate without queries if sync on time", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		publish := types.EpochID(2)
		now := time.Now()
		tester.clock.EXPECT().LayerToTime(publish.FirstLayer()).Return(now).AnyTimes()

		state := map[types.ATXID]int{
			aid("1"): 0,
			aid("2"): 0,
		}
		require.NoError(t, atxsync.SaveSyncState(tester.localdb, publish, state, tester.cfg.AtxsBatch))
		lastSuccess := now.Add(-tester.cfg.EpochInfoInterval)
		require.NoError(t, atxsync.SaveRequestTime(tester.localdb, publish, lastSuccess))
		for id := range state {
			require.NoError(t, atxs.Add(tester.db, atx(id)))
		}
		require.NoError(t, tester.syncer.Download(context.Background(), publish))
	})
	t.Run("immediate epoch info retries", func(t *testing.T) {
		tester := newTester(t, Config{
			EpochInfoInterval: 10 * time.Second,
			EpochInfoPeers:    3,
			RequestsLimit:     10,
			AtxsBatch:         10,
		})
		ctx, cancel := context.WithTimeout(context.Background(), tester.cfg.EpochInfoInterval/2)
		defer cancel()

		peers := []p2p.Peer{"a"}
		tester.fetcher.EXPECT().SelectBestShuffled(tester.cfg.EpochInfoPeers).Return(peers).AnyTimes()
		publish := types.EpochID(2)
		tester.fetcher.EXPECT().PeerEpochInfo(gomock.Any(), peers[0], publish).Return(nil, errors.New("bad try"))
		tester.fetcher.EXPECT().PeerEpochInfo(gomock.Any(), peers[0], publish).Return(edata("1", "2", "3"), nil)

		tester.fetcher.EXPECT().
			GetAtxs(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, ids []types.ATXID, _ ...system.GetAtxOpt) error {
				for _, id := range ids {
					require.NoError(t, atxs.Add(tester.db, atx(id)))
				}
				return nil
			}).AnyTimes()

		past := time.Now().Add(-time.Minute)
		tester.clock.EXPECT().LayerToTime(publish.FirstLayer()).Return(past).AnyTimes()
		require.NoError(t, tester.syncer.Download(ctx, publish))
	})
	t.Run("give up on atx after max retries", func(t *testing.T) {
		tester := newTester(t, Config{
			EpochInfoInterval: 200 * time.Hour,
			EpochInfoPeers:    2,
			RequestsLimit:     10,
			AtxsBatch:         2,
		})

		peers := []p2p.Peer{"a", "b"}
		tester.fetcher.EXPECT().SelectBestShuffled(tester.cfg.EpochInfoPeers).Return(peers).AnyTimes()
		publish := types.EpochID(2)
		good := edata("1", "2", "3")
		bad := edata("4", "5", "6")
		tester.fetcher.EXPECT().PeerEpochInfo(gomock.Any(), peers[0], publish).Return(good, nil)
		tester.fetcher.EXPECT().PeerEpochInfo(gomock.Any(), peers[1], publish).Return(bad, nil)

		tester.fetcher.EXPECT().
			GetAtxs(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, ids []types.ATXID, _ ...system.GetAtxOpt) error {
				require.LessOrEqual(t, len(ids), tester.cfg.AtxsBatch)
				berr := fetch.BatchError{}
				for _, id := range ids {
					for _, good := range good.AtxIDs {
						if good == id {
							require.NoError(t, atxs.Add(tester.db, atx(id)))
						}
					}
					for _, bad := range bad.AtxIDs {
						if bad == id {
							berr.Add(bad.Hash32(), fmt.Errorf("%w: test", server.ErrPeerResponseFailed))
						}
					}
				}
				if berr.Empty() {
					return nil
				}
				return &berr
			}).
			AnyTimes()

		past := time.Now().Add(-time.Minute)
		tester.clock.EXPECT().LayerToTime(publish.FirstLayer()).Return(past).AnyTimes()
		require.NoError(t, tester.syncer.Download(context.Background(), publish))

		state, err := atxsync.GetSyncState(tester.localdb, publish)
		require.NoError(t, err)
		for _, bad := range bad.AtxIDs {
			require.NotContains(t, state, bad)
		}
	})
	t.Run("terminate empty epoch", func(t *testing.T) {
		tester := newTester(t, DefaultConfig())
		publish := types.EpochID(2)
		now := time.Now()
		tester.clock.EXPECT().LayerToTime(publish.FirstLayer()).Return(now).AnyTimes()
		peers := []p2p.Peer{"a"}
		tester.fetcher.EXPECT().SelectBestShuffled(tester.cfg.EpochInfoPeers).Return(peers).AnyTimes()
		tester.fetcher.EXPECT().PeerEpochInfo(gomock.Any(), peers[0], publish).Return(edata(), nil)
		require.NoError(t, tester.syncer.Download(context.Background(), publish))
	})
}
