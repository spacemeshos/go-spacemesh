package syncer_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/mocks"
)

type testForkFinder struct {
	*syncer.ForkFinder
	db       *sql.Database
	mFetcher *mocks.Mockfetcher
}

func newTestForkFinder(t *testing.T, maxHashes uint32) *testForkFinder {
	return newTestForkFinderWithDuration(t, maxHashes, time.Hour, logtest.New(t))
}

func newTestForkFinderWithDuration(t *testing.T, maxHashes uint32, d time.Duration, lg log.Log) *testForkFinder {
	mf := mocks.NewMockfetcher(gomock.NewController(t))
	db := sql.InMemory()
	require.NoError(t, layers.SetMeshHash(db, types.GetEffectiveGenesis(), types.RandomHash()))
	return &testForkFinder{
		ForkFinder: syncer.NewForkFinder(lg, db, mf, maxHashes, d),
		db:         db,
		mFetcher:   mf,
	}
}

func TestResynced(t *testing.T) {
	tf := newTestForkFinderWithDuration(t, 5, 0, logtest.New(t))
	lid := types.LayerID(11)
	hash := types.RandomHash()
	require.True(t, tf.NeedResync(lid, hash))
	tf.AddResynced(lid, hash)
	require.False(t, tf.NeedResync(lid, hash))

	tf.mFetcher.EXPECT().GetPeers().Return([]p2p.Peer{})
	tf.Purge(false)
	require.True(t, tf.NeedResync(lid, hash))
}

func TestForkFinder_Purge(t *testing.T) {
	tf := newTestForkFinder(t, 5)
	numCached := 10
	tf.UpdateAgreement(p2p.Peer(strconv.Itoa(0)), types.LayerID(uint32(1)), types.RandomHash(), time.Now().Add(-2*time.Hour))
	for i := 1; i < numCached; i++ {
		tf.UpdateAgreement(p2p.Peer(strconv.Itoa(i)), types.LayerID(uint32(i+1)), types.RandomHash(), time.Now())
	}
	tf.mFetcher.EXPECT().GetPeers().Return([]p2p.Peer{})
	require.Equal(t, numCached, tf.NumPeersCached())
	tf.Purge(false)
	require.Equal(t, 9, tf.NumPeersCached())
	tf.Purge(false, p2p.Peer(strconv.Itoa(numCached-1)), p2p.Peer(strconv.Itoa(numCached-2)))
	require.Equal(t, 7, tf.NumPeersCached())
	tf.Purge(true)
	require.Equal(t, 0, tf.NumPeersCached())
}

func layerHash(layer int, good bool) types.Hash32 {
	var h2 types.Hash32
	h := h2[:0]
	if good {
		h = append(h, "good/"...)
		binary.LittleEndian.AppendUint32(h, uint32(layer))
	} else {
		h = append(h, "bad/"...)
		binary.LittleEndian.AppendUint32(h, uint32(layer))
	}
	return h2
}

func storeNodeHashes(t *testing.T, db *sql.Database, diverge, max int) {
	for lid := 0; lid <= max; lid++ {
		if lid < diverge {
			require.NoError(t, layers.SetMeshHash(db, types.LayerID(uint32(lid)), layerHash(lid, true)))
		} else {
			require.NoError(t, layers.SetMeshHash(db, types.LayerID(uint32(lid)), layerHash(lid, false)))
		}
	}
}

func serveHashReq(t *testing.T, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
	var (
		lids   = []types.LayerID{req.From}
		hashes = []types.Hash32{layerHash(int(req.From.Uint32()), true)}
		steps  uint32
		lid    = req.From.Add(req.Delta)
	)
	for ; ; lid = lid.Add(req.Delta) {
		steps++
		if !lid.Before(req.To) {
			lids = append(lids, req.To)
			hashes = append(hashes, layerHash(int(req.To.Uint32()), true))
			break
		}
		lids = append(lids, lid)
		hashes = append(hashes, layerHash(int(lid.Uint32()), true))
	}
	require.Equal(t, len(lids), len(hashes))
	require.Equal(t, req.Steps, steps, fmt.Sprintf("exp: %v, got %v", req.Steps, steps))
	mh := &fetch.MeshHashes{
		Layers: lids,
		Hashes: hashes,
	}
	return mh, nil
}

func TestForkFinder_FindFork_Permutation(t *testing.T) {
	peer := p2p.Peer("grumpy")
	max := 173
	diverge := rand.Intn(max)
	gLid := types.GetEffectiveGenesis()
	if diverge <= int(gLid.Uint32()) {
		diverge = int(gLid.Uint32()) + 1
	}
	expected := diverge - 1
	for maxHashes := uint32(30); maxHashes >= 5; maxHashes -= 3 {
		for lid := max; lid > expected; lid-- {
			tf := newTestForkFinderWithDuration(t, maxHashes, time.Hour, logtest.New(t))
			storeNodeHashes(t, tf.db, diverge, max)
			tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
					return serveHashReq(t, req)
				}).AnyTimes()

			fork, err := tf.FindFork(context.TODO(), peer, types.LayerID(uint32(lid)), layerHash(lid, true))
			require.NoError(t, err)
			require.EqualValues(t, expected, fork.Uint32())
		}
	}
}

func TestForkFinder_MeshChangedMidSession(t *testing.T) {
	maxHashes := uint32(100)
	peer := p2p.Peer("grumpy")
	lastAgreedLid := types.LayerID(35)
	lastAgreedHash := types.RandomHash()

	t.Run("peer mesh changed", func(t *testing.T) {
		t.Parallel()

		tf := newTestForkFinder(t, maxHashes)
		require.NoError(t, layers.SetMeshHash(tf.db, lastAgreedLid, lastAgreedHash))
		tf.UpdateAgreement(peer, lastAgreedLid, lastAgreedHash, time.Now())
		tf.UpdateAgreement("shorty", types.LayerID(111), types.RandomHash(), time.Now())
		require.Equal(t, tf.NumPeersCached(), 2)
		tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
				mh := &fetch.MeshHashes{
					Layers: []types.LayerID{types.LayerID(35), types.LayerID(36), types.LayerID(37)},
					Hashes: []types.Hash32{types.RandomHash(), types.RandomHash(), types.RandomHash()},
				}
				return mh, nil
			})

		_, err := tf.FindFork(context.TODO(), peer, types.LayerID(37), types.RandomHash())
		require.ErrorIs(t, err, syncer.ErrPeerMeshChangedMidSession)
		require.Equal(t, tf.NumPeersCached(), 1)
	})

	t.Run("node mesh changed", func(t *testing.T) {
		t.Parallel()

		tf := newTestForkFinder(t, maxHashes)
		require.NoError(t, layers.SetMeshHash(tf.db, lastAgreedLid, lastAgreedHash))
		tf.UpdateAgreement(peer, lastAgreedLid, lastAgreedHash, time.Now())
		tf.UpdateAgreement("shorty", types.LayerID(111), types.RandomHash(), time.Now())
		require.Equal(t, tf.NumPeersCached(), 2)
		lastDiffLid := types.LayerID(37)
		lastDiffHash := types.RandomHash()
		tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
				mh := &fetch.MeshHashes{
					Layers: []types.LayerID{types.LayerID(35), types.LayerID(36), types.LayerID(37)},
					Hashes: []types.Hash32{lastAgreedHash, types.RandomHash(), lastDiffHash},
				}
				// changes the node's own hash for lastAgreedLid
				for _, lid := range mh.Layers {
					require.NoError(t, layers.SetMeshHash(tf.db, lid, types.RandomHash()))
				}
				return mh, nil
			})

		_, err := tf.FindFork(context.TODO(), peer, lastDiffLid, lastDiffHash)
		require.ErrorIs(t, err, syncer.ErrNodeMeshChangedMidSession)
		require.Equal(t, tf.NumPeersCached(), 0)
	})
}

func TestForkFinder_FindFork_Edges(t *testing.T) {
	const (
		max     = 20
		diverge = 12
	)
	tt := []struct {
		name               string
		lastSame, lastDiff int
		expReqs            int
	}{
		{
			name:     "no prior hash agreement",
			lastDiff: 20,
			expReqs:  2,
		},
		{
			name:     "prior agreement",
			lastDiff: 20,
			lastSame: 8,
			expReqs:  2,
		},
		{
			name:     "immediate detection",
			lastDiff: 12,
			lastSame: 11,
			expReqs:  0,
		},
	}
	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			maxHashes := uint32(5)
			tf := newTestForkFinder(t, maxHashes)
			storeNodeHashes(t, tf.db, diverge, max)

			peer := p2p.Peer("grumpy")
			if tc.lastSame != 0 {
				tf.UpdateAgreement(peer, types.LayerID(uint32(tc.lastSame)), layerHash(tc.lastSame, true), time.Now())
			}

			tf.mFetcher.EXPECT().PeerMeshHashes(gomock.Any(), peer, gomock.Any()).DoAndReturn(
				func(_ context.Context, _ p2p.Peer, req *fetch.MeshHashRequest) (*fetch.MeshHashes, error) {
					return serveHashReq(t, req)
				}).Times(tc.expReqs)

			fork, err := tf.FindFork(context.TODO(), peer, types.LayerID(uint32(tc.lastDiff)), layerHash(tc.lastDiff, true))
			require.NoError(t, err)
			require.Equal(t, types.LayerID(11), fork)
		})
	}
}
