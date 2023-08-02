package hare3

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/benbjohnson/clock"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	pmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/beacons"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

const layersPerEpoch = 4

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(layersPerEpoch)
	res := m.Run()
	os.Exit(res)
}

func testHare(tb testing.TB, n int, pause time.Duration) {
	tb.Helper()
	now := time.Now()
	cfg := DefaultConfig()
	layerDuration := 5 * time.Minute
	beacon := types.Beacon{1, 1, 1, 1}

	clocks := make([]*clock.Mock, n)
	for i := 0; i < n; i++ {
		clocks[i] = clock.NewMock()
		clocks[i].Set(now)
	}
	verifier, err := signing.NewEdVerifier()
	require.NoError(tb, err)
	vrfverifier := signing.NewVRFVerifier()
	rng := rand.New(rand.NewSource(1001))
	hares := make([]*Hare, n)
	tb.Cleanup(func() {
		for _, hr := range hares {
			hr.Stop()
		}
	})
	signers := make([]*signing.EdSigner, n)
	for i := range signers {
		signer, err := signing.NewEdSigner(signing.WithKeyFromRand(rng))
		require.NoError(tb, err)
		signers[i] = signer
	}
	genesis := types.GetEffectiveGenesis()
	vatxs := make([]*types.VerifiedActivationTx, n)
	ids := make([]types.ATXID, n)
	for i := range vatxs {
		atx := &types.ActivationTx{}
		atx.NumUnits = 10
		atx.PublishEpoch = genesis.GetEpoch()
		atx.SmesherID = signers[i].NodeID()
		id := types.ATXID{}
		rng.Read(id[:])
		atx.SetID(id)
		atx.SetEffectiveNumUnits(atx.NumUnits)
		atx.SetReceived(now)
		nonce := types.VRFPostIndex(rng.Uint64())
		atx.VRFNonce = &nonce
		verified, err := atx.Verify(0, 100)
		require.NoError(tb, err)
		vatxs[i] = verified
		ids[i] = id
	}

	for i := 0; i < n; i++ {
		logger := logtest.New(tb).Named(fmt.Sprintf("hare=%d", i))
		ctrl := gomock.NewController(tb)
		syncer := smocks.NewMockSyncStateProvider(ctrl)
		syncer.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
		db := datastore.NewCachedDB(sql.InMemory(), log.NewNop())
		require.NoError(tb, beacons.Add(db, types.GetEffectiveGenesis().GetEpoch()+1, beacon))
		beaconget := smocks.NewMockBeaconGetter(ctrl)
		beaconget.EXPECT().GetBeacon(gomock.Any()).DoAndReturn(func(epoch types.EpochID) (types.Beacon, error) {
			return beacons.Get(db, epoch)
		}).AnyTimes()
		for _, atx := range vatxs {
			require.NoError(tb, atxs.Add(db, atx))
		}
		vrfsigner, err := signers[i].VRFSigner()
		require.NoError(tb, err)
		or := eligibility.New(beaconget, db, vrfverifier, vrfsigner, layersPerEpoch, config.DefaultConfig(), log.NewNop())
		or.UpdateActiveSet(types.FirstEffectiveGenesis().GetEpoch()+1, ids)
		nodeclock, err := timesync.NewClock(
			timesync.WithLogger(log.NewNop()),
			timesync.WithClock(clocks[i]),
			timesync.WithGenesisTime(now),
			timesync.WithLayerDuration(layerDuration),
			timesync.WithTickInterval(layerDuration),
		)
		require.NoError(tb, err)
		pubs := pmocks.NewMockPublishSubsciber(ctrl)
		pubs.EXPECT().Register(gomock.Any(), gomock.Any()).AnyTimes()
		pubs.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).Do(func(ctx context.Context, _ string, msg []byte) error {
			for _, hr := range hares {
				require.NoError(tb, hr.handler(ctx, "self", msg))
			}
			return nil
		}).AnyTimes()
		hares[i] = New(nodeclock, pubs, db, verifier, signers[i], or, syncer,
			WithConfig(cfg),
			WithLogger(logger.Zap()),
			WithWallclock(clocks[i]),
			WithEnableLayer(genesis),
		)
	}
	for i := range hares {
		hares[i].Start()
	}
	layer := types.GetEffectiveGenesis() + 1
	bound := len(vatxs)
	if bound > 50 {
		bound = 50
	}
	for i, atx := range vatxs[:bound] {
		proposal := &types.Proposal{}
		proposal.Layer = layer
		proposal.ActiveSet = ids
		proposal.EpochData = &types.EpochData{
			Beacon: beacon,
		}
		proposal.AtxID = atx.ID()
		proposal.SmesherID = signers[i].NodeID()
		id := types.ProposalID{}
		rng.Read(id[:])
		bid := types.BallotID{}
		rng.Read(bid[:])
		proposal.SetID(id)
		proposal.Ballot.SetID(bid)
		for _, hr := range hares {
			require.NoError(tb, ballots.Add(hr.db, &proposal.Ballot))
			require.NoError(tb, proposals.Add(hr.db, proposal))
		}
	}
	target := hares[0].nodeclock.LayerToTime(genesis.Add(1)).Add(2 * time.Second)
	for _, wall := range clocks {
		// this triggers layer event
		wall.Set(target)
	}
	target = target.Add(cfg.PreroundDelay)
	for _, wall := range clocks {
		wall.Set(target)
	}
	for i := 0; i < 2*int(notify); i++ {
		// TODO(dshulyak) this needs to be improved, i lack synchronization
		// when active members completed round
		time.Sleep(pause)
		target = target.Add(cfg.RoundDuration)
		for _, wall := range clocks {
			wall.Set(target)
		}
	}
	for _, hr := range hares {
		select {
		case coin := <-hr.Coins():
			require.Equal(tb, coin.Layer, layer)
		default:
			require.FailNow(tb, "no coin")
		}
		select {
		case rst := <-hr.Results():
			require.Equal(tb, rst.Layer, layer)
			require.NotEmpty(tb, rst.Proposals)
		default:
			require.FailNow(tb, "no result")
		}
	}

	for _, hr := range hares {
		time.Sleep(pause)
		require.Empty(tb, hr.Running())
	}
}

func TestHare(t *testing.T) {
	t.Run("two", func(t *testing.T) { testHare(t, 2, 50*time.Millisecond) })
	t.Run("small", func(t *testing.T) { testHare(t, 5, 50*time.Millisecond) })
}
