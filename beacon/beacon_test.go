package beacon

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/spacemeshos/fixed"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/beacon/metrics"
	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/types/result"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/system/mocks"
)

func coinValueMock(tb testing.TB, value bool) coin {
	ctrl := gomock.NewController(tb)
	coinMock := NewMockcoin(ctrl)
	coinMock.EXPECT().StartEpoch(
		gomock.Any(),
		gomock.AssignableToTypeOf(types.EpochID(0)),
	).AnyTimes()
	coinMock.EXPECT().FinishEpoch(gomock.Any(), gomock.AssignableToTypeOf(types.EpochID(0))).AnyTimes()
	coinMock.EXPECT().StartRound(gomock.Any(),
		gomock.AssignableToTypeOf(types.RoundID(0)),
		gomock.AssignableToTypeOf([]weakcoin.Participant{}),
	).AnyTimes()
	coinMock.EXPECT().FinishRound(gomock.Any()).AnyTimes()
	coinMock.EXPECT().Get(
		gomock.Any(),
		gomock.AssignableToTypeOf(types.EpochID(0)),
		gomock.AssignableToTypeOf(types.RoundID(0)),
	).AnyTimes().Return(value, nil)
	return coinMock
}

func newPublisher(tb testing.TB) pubsub.Publisher {
	tb.Helper()
	ctrl := gomock.NewController(tb)

	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().
		Publish(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()
	return publisher
}

type testProtocolDriver struct {
	*ProtocolDriver
	ctrl      *gomock.Controller
	cdb       *datastore.CachedDB
	mClock    *MocklayerClock
	mSync     *mocks.MockSyncStateProvider
	mVerifier *MockvrfVerifier
}

func setUpProtocolDriver(tb testing.TB) *testProtocolDriver {
	return newTestDriver(tb, UnitTestConfig(), newPublisher(tb), 3, "")
}

func newTestDriver(tb testing.TB, cfg Config, p pubsub.Publisher, miners int, id string) *testProtocolDriver {
	ctrl := gomock.NewController(tb)
	tpd := &testProtocolDriver{
		ctrl:      ctrl,
		mClock:    NewMocklayerClock(ctrl),
		mSync:     mocks.NewMockSyncStateProvider(ctrl),
		mVerifier: NewMockvrfVerifier(ctrl),
	}
	lg := zaptest.NewLogger(tb).Named(id)

	tpd.mVerifier.EXPECT().Verify(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(true)

	tpd.cdb = datastore.NewCachedDB(sql.InMemory(), lg)
	tpd.ProtocolDriver = New(p, signing.NewEdVerifier(), tpd.mVerifier, tpd.cdb, tpd.mClock,
		WithConfig(cfg),
		WithLogger(lg),
		withWeakCoin(coinValueMock(tb, true)),
	)
	tpd.ProtocolDriver.SetSyncState(tpd.mSync)
	for i := 0; i < miners; i++ {
		edSgn, err := signing.NewEdSigner()
		require.NoError(tb, err)
		tpd.ProtocolDriver.Register(edSgn)
	}
	return tpd
}

func createATX(
	tb testing.TB,
	db *datastore.CachedDB,
	lid types.LayerID,
	sig *signing.EdSigner,
	numUnits uint32,
	received time.Time,
) types.ATXID {
	nonce := types.VRFPostIndex(1)
	atx := types.NewActivationTx(
		types.NIPostChallenge{PublishEpoch: lid.GetEpoch()},
		types.GenerateAddress(types.RandomBytes(types.AddressLength)),
		numUnits,
	)
	atx.VRFNonce = nonce
	atx.SetReceived(received)
	atx.SmesherID = sig.NodeID()
	atx.SetID(types.RandomATXID())
	atx.TickCount = 1
	require.NoError(tb, atxs.Add(db, atx, types.AtxBlob{}))
	return atx.ID()
}

func createRandomATXs(tb testing.TB, db *datastore.CachedDB, lid types.LayerID, num int) {
	for i := 0; i < num; i++ {
		sig, err := signing.NewEdSigner()
		require.NoError(tb, err)
		createATX(tb, db, lid, sig, 1, time.Now().Add(-1*time.Second))
	}
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func TestBeacon_MultipleNodes(t *testing.T) {
	numNodes := 5
	numMinersPerNode := 7
	testNodes := make([]*testProtocolDriver, 0, numNodes)
	publisher := pubsubmocks.NewMockPublisher(gomock.NewController(t))
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, protocol string, data []byte) error {
			for i, node := range testNodes {
				peer := p2p.Peer(strconv.Itoa(i))
				switch protocol {
				case pubsub.BeaconProposalProtocol:
					require.NoError(t, node.HandleProposal(ctx, peer, data))
				case pubsub.BeaconFirstVotesProtocol:
					require.NoError(t, node.HandleFirstVotes(ctx, peer, data))
				case pubsub.BeaconFollowingVotesProtocol:
					require.NoError(t, node.HandleFollowingVotes(ctx, peer, data))
				case pubsub.BeaconWeakCoinProtocol:
				}
			}
			return nil
		}).AnyTimes()

	atxPublishLid := types.LayerID(types.GetLayersPerEpoch()*2 - 1)
	current := atxPublishLid.Add(1)
	dbs := make([]*datastore.CachedDB, 0, numNodes)
	cfg := NodeSimUnitTestConfig()
	bootstrap := types.Beacon{1, 2, 3, 4}
	now := time.Now()
	for i := 0; i < numNodes; i++ {
		node := newTestDriver(t, cfg, publisher, numMinersPerNode, fmt.Sprintf("node-%d", i))
		require.NoError(t, node.UpdateBeacon(types.EpochID(2), bootstrap))
		node.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
		node.mClock.EXPECT().CurrentLayer().Return(current).AnyTimes()
		node.mClock.EXPECT().LayerToTime(current).Return(now).AnyTimes()
		testNodes = append(testNodes, node)
		dbs = append(dbs, node.cdb)

		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(0)), errGenesis)
		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(1)), errGenesis)
		got, err := node.GetBeacon(types.EpochID(2))
		require.NoError(t, err)
		require.Equal(t, bootstrap, got)
	}
	for i, node := range testNodes {
		if i == 0 {
			// make the first node non-smeshing node
			continue
		}

		for _, db := range dbs {
			for _, s := range node.signers {
				createATX(t, db, atxPublishLid, s, 1, time.Now().Add(-1*time.Second))
			}
		}
	}
	var eg errgroup.Group
	for _, node := range testNodes {
		eg.Go(func() error {
			return node.onNewEpoch(context.Background(), types.EpochID(2))
		})
	}
	require.NoError(t, eg.Wait())
	beacons := make(map[types.Beacon]struct{})
	for _, node := range testNodes {
		got, err := node.GetBeacon(types.EpochID(3))
		require.NoError(t, err)
		require.NotEqual(t, types.EmptyBeacon, got)
		beacons[got] = struct{}{}
	}
	require.Len(t, beacons, 1)
}

func TestBeacon_MultipleNodes_OnlyOneHonest(t *testing.T) {
	numNodes := 5
	testNodes := make([]*testProtocolDriver, 0, numNodes)
	publisher := pubsubmocks.NewMockPublisher(gomock.NewController(t))
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, protocol string, data []byte) error {
			for i, node := range testNodes {
				switch protocol {
				case pubsub.BeaconProposalProtocol:
					require.NoError(t, node.HandleProposal(ctx, p2p.Peer(strconv.Itoa(i)), data))
				case pubsub.BeaconFirstVotesProtocol:
					require.NoError(t, node.HandleFirstVotes(ctx, p2p.Peer(strconv.Itoa(i)), data))
				case pubsub.BeaconFollowingVotesProtocol:
					require.NoError(t, node.HandleFollowingVotes(ctx, p2p.Peer(strconv.Itoa(i)), data))
				case pubsub.BeaconWeakCoinProtocol:
				}
			}
			return nil
		}).AnyTimes()

	atxPublishLid := types.LayerID(types.GetLayersPerEpoch()*2 - 1)
	current := atxPublishLid.Add(1)
	dbs := make([]*datastore.CachedDB, 0, numNodes)
	cfg := NodeSimUnitTestConfig()
	bootstrap := types.Beacon{1, 2, 3, 4}
	now := time.Now()
	for i := 0; i < numNodes; i++ {
		node := newTestDriver(t, cfg, publisher, 3, fmt.Sprintf("node-%d", i))
		require.NoError(t, node.UpdateBeacon(types.EpochID(2), bootstrap))
		node.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
		node.mClock.EXPECT().CurrentLayer().Return(current).AnyTimes()
		node.mClock.EXPECT().LayerToTime(current).Return(now).AnyTimes()
		testNodes = append(testNodes, node)
		dbs = append(dbs, node.cdb)

		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(0)), errGenesis)
		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(1)), errGenesis)
		got, err := node.GetBeacon(types.EpochID(2))
		require.NoError(t, err)
		require.Equal(t, bootstrap, got)
	}
	for i, node := range testNodes {
		for _, db := range dbs {
			for _, s := range node.signers {
				createATX(t, db, atxPublishLid, s, 1, time.Now().Add(-1*time.Second))
				if i != 0 {
					require.NoError(t, identities.SetMalicious(db, s.NodeID(), []byte("bad"), time.Now()))
				}
			}
		}
	}
	var eg errgroup.Group
	for _, node := range testNodes {
		eg.Go(func() error {
			return node.onNewEpoch(context.Background(), types.EpochID(2))
		})
	}
	require.NoError(t, eg.Wait())
	beacons := make(map[types.Beacon]struct{})
	for _, node := range testNodes {
		got, err := node.GetBeacon(types.EpochID(3))
		require.NoError(t, err)
		require.NotEqual(t, types.EmptyBeacon, got)
		beacons[got] = struct{}{}
	}
	require.Len(t, beacons, 1)
}

func TestBeacon_NoProposals(t *testing.T) {
	numNodes := 5
	testNodes := make([]*testProtocolDriver, 0, numNodes)
	publisher := pubsubmocks.NewMockPublisher(gomock.NewController(t))
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	atxPublishLid := types.LayerID(types.GetLayersPerEpoch()*2 - 1)
	current := atxPublishLid.Add(1)
	dbs := make([]*datastore.CachedDB, 0, numNodes)
	cfg := NodeSimUnitTestConfig()
	now := time.Now()
	bootstrap := types.Beacon{1, 2, 3, 4}
	for i := 0; i < numNodes; i++ {
		node := newTestDriver(t, cfg, publisher, 3, fmt.Sprintf("node-%d", i))
		require.NoError(t, node.UpdateBeacon(types.EpochID(2), bootstrap))
		node.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
		node.mClock.EXPECT().CurrentLayer().Return(current).AnyTimes()
		node.mClock.EXPECT().LayerToTime(current).Return(now).AnyTimes()
		testNodes = append(testNodes, node)
		dbs = append(dbs, node.cdb)

		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(0)), errGenesis)
		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(1)), errGenesis)
		got, err := node.GetBeacon(types.EpochID(2))
		require.NoError(t, err)
		require.Equal(t, bootstrap, got)
	}
	for _, node := range testNodes {
		for _, db := range dbs {
			for _, s := range node.signers {
				createATX(t, db, atxPublishLid, s, 1, time.Now().Add(-1*time.Second))
			}
		}
	}
	var eg errgroup.Group
	for _, node := range testNodes {
		eg.Go(func() error {
			return node.onNewEpoch(context.Background(), types.EpochID(2))
		})
	}
	require.NoError(t, eg.Wait())
	for _, node := range testNodes {
		got, err := node.GetBeacon(types.EpochID(3))
		require.Error(t, err)
		require.Equal(t, types.EmptyBeacon, got)
	}
}

func getNoWait(tb testing.TB, results <-chan result.Beacon) result.Beacon {
	select {
	case rst := <-results:
		return rst
	default:
	}
	require.Fail(tb, "beacon is not available")
	return result.Beacon{}
}

func TestBeaconNotSynced(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	require.ErrorIs(t, tpd.onNewEpoch(context.Background(), types.EpochID(0)), errGenesis)
	require.ErrorIs(t, tpd.onNewEpoch(context.Background(), types.EpochID(1)), errGenesis)
	require.ErrorIs(t, tpd.onNewEpoch(context.Background(), types.EpochID(2)), errNodeNotSynced)

	got, err := tpd.GetBeacon(types.EpochID(2))
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)

	bootstrap := types.Beacon{1, 2, 3, 4}
	require.NoError(t, tpd.UpdateBeacon(types.EpochID(2), bootstrap))
	got, err = tpd.GetBeacon(types.EpochID(2))
	require.Equal(t, got, getNoWait(t, tpd.Results()).Beacon)

	require.NoError(t, err)
	require.Equal(t, bootstrap, got)

	got, err = tpd.GetBeacon(types.EpochID(3))
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeaconNotSynced_ReleaseMemory(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.config.BeaconSyncWeightUnits = 1
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	start := types.EpochID(2)
	end := start + numEpochsToKeep + 10
	tpd.mClock.EXPECT().CurrentLayer().Return(start.FirstLayer()).AnyTimes()
	for eid := start; eid <= end; eid++ {
		b := types.NewExistingBallot(
			types.RandomBallotID(),
			types.EmptyEdSignature,
			types.EmptyNodeID,
			start.FirstLayer(),
		)
		b.EligibilityProofs = []types.VotingEligibility{{J: 1}}
		tpd.ReportBeaconFromBallot(eid, &b, types.RandomBeacon(), fixed.New64(1))
		require.ErrorIs(t, tpd.onNewEpoch(context.Background(), eid), errNodeNotSynced)
	}
	require.Len(t, tpd.beacons, numEpochsToKeep)
	require.Len(t, tpd.ballotsBeacons, numEpochsToKeep)
}

func TestBeaconNoATXInPreviousEpoch(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	require.ErrorIs(t, tpd.onNewEpoch(context.Background(), types.EpochID(0)), errGenesis)
	require.ErrorIs(t, tpd.onNewEpoch(context.Background(), types.EpochID(1)), errGenesis)
	tpd.mClock.EXPECT().LayerToTime(types.EpochID(2).FirstLayer()).Return(time.Now())
	require.ErrorIs(t, errZeroEpochWeight, tpd.onNewEpoch(context.Background(), types.EpochID(2)))
}

func TestBeaconWithMetrics(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()

	gLayer := types.GetEffectiveGenesis()
	tpd.mClock.EXPECT().CurrentLayer().Return(gLayer).Times(2)
	tpd.mClock.EXPECT().AwaitLayer(gLayer.Add(1)).Return(nil).Times(1)
	tpd.mClock.EXPECT().LayerToTime((gLayer.GetEpoch() + 1).FirstLayer()).Return(time.Now()).AnyTimes()
	tpd.Start(context.Background())

	epoch := types.EpochID(3)
	for i := types.EpochID(2); i < epoch; i++ {
		lid := i.FirstLayer().Sub(1)
		for _, s := range tpd.signers {
			createATX(t, tpd.cdb, lid, s, 199, time.Now())
		}
		createRandomATXs(t, tpd.cdb, lid, 9)
	}
	finalLayer := types.LayerID(types.GetLayersPerEpoch() * uint32(epoch))
	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	for layer := gLayer.Add(1); layer.Before(finalLayer); layer = layer.Add(1) {
		tpd.mClock.EXPECT().CurrentLayer().Return(layer).AnyTimes()
		if layer.FirstInEpoch() {
			require.NoError(t, tpd.onNewEpoch(context.Background(), layer.GetEpoch()))
		}
		thisEpoch := layer.GetEpoch()
		b := types.NewExistingBallot(
			types.RandomBallotID(),
			types.EmptyEdSignature,
			types.EmptyNodeID,
			thisEpoch.FirstLayer(),
		)
		b.EligibilityProofs = []types.VotingEligibility{{J: 1}}
		tpd.recordBeacon(thisEpoch, &b, beacon1, fixed.New64(1))
		b = types.NewExistingBallot(
			types.RandomBallotID(),
			types.EmptyEdSignature,
			types.EmptyNodeID,
			thisEpoch.FirstLayer(),
		)
		b.EligibilityProofs = []types.VotingEligibility{{J: 1}}
		tpd.recordBeacon(thisEpoch, &b, beacon2, fixed.New64(1))

		count := layer.OrdinalInEpoch() + 1
		//nolint:lll
		expected := fmt.Sprintf(`
			# HELP spacemesh_beacons_beacon_observed_total Number of beacons collected from blocks for each epoch and value
			# TYPE spacemesh_beacons_beacon_observed_total counter
			spacemesh_beacons_beacon_observed_total{beacon="%s",epoch="%d"} %d
			spacemesh_beacons_beacon_observed_total{beacon="%s",epoch="%d"} %d
			`,
			beacon1.ShortString(), thisEpoch, count,
			beacon2.ShortString(), thisEpoch, count,
		)
		err := testutil.GatherAndCompare(
			prometheus.DefaultGatherer,
			strings.NewReader(expected),
			"spacemesh_beacons_beacon_observed_total",
		)
		require.NoError(t, err)

		weight := layer.OrdinalInEpoch() + 1
		//nolint:lll
		expected = fmt.Sprintf(`
			# HELP spacemesh_beacons_beacon_observed_weight Weight of beacons collected from blocks for each epoch and value
			# TYPE spacemesh_beacons_beacon_observed_weight counter
			spacemesh_beacons_beacon_observed_weight{beacon="%s",epoch="%d"} %d
			spacemesh_beacons_beacon_observed_weight{beacon="%s",epoch="%d"} %d
			`,
			beacon1.ShortString(), thisEpoch, weight,
			beacon2.ShortString(), thisEpoch, weight,
		)
		err = testutil.GatherAndCompare(
			prometheus.DefaultGatherer,
			strings.NewReader(expected),
			"spacemesh_beacons_beacon_observed_weight",
		)
		require.NoError(t, err)

		calcWeightCount, err := testutil.GatherAndCount(
			prometheus.DefaultGatherer,
			"spacemesh_beacons_beacon_calculated_weight",
		)
		require.NoError(t, err)
		require.Zero(t, calcWeightCount)
	}

	tpd.Close()
}

func TestBeacon_NoRaceOnClose(t *testing.T) {
	mclock := NewMocklayerClock(gomock.NewController(t))
	lg := zaptest.NewLogger(t)
	pd := &ProtocolDriver{
		logger:           lg.Named("Beacon"),
		beacons:          make(map[types.EpochID]types.Beacon),
		cdb:              datastore.NewCachedDB(sql.InMemory(), lg),
		clock:            mclock,
		closed:           make(chan struct{}),
		results:          make(chan result.Beacon, 100),
		metricsCollector: metrics.NewBeaconMetricsCollector(nil, lg.Named("metrics")),
	}
	// check for a race between onResult and Close
	var eg errgroup.Group
	eg.Go(func() error {
		for i := 0; i < 1000; i++ {
			time.Sleep(1 * time.Millisecond)
			pd.onResult(types.EpochID(i), types.Beacon{})
		}
		return nil
	})
	eg.Go(func() error {
		resultChan := pd.Results()
		for result := range resultChan {
			t.Log(result)
		}
		return nil
	})
	time.Sleep(300 * time.Millisecond)
	pd.Close()
	require.NoError(t, eg.Wait())
}

func TestBeacon_BeaconsWithDatabase(t *testing.T) {
	t.Parallel()

	lg := zaptest.NewLogger(t)
	mclock := NewMocklayerClock(gomock.NewController(t))
	pd := &ProtocolDriver{
		logger:  lg.Named("Beacon"),
		beacons: make(map[types.EpochID]types.Beacon),
		cdb:     datastore.NewCachedDB(sql.InMemory(), lg),
		clock:   mclock,
	}
	epoch3 := types.EpochID(3)
	beacon2 := types.RandomBeacon()
	epoch5 := types.EpochID(5)
	beacon4 := types.RandomBeacon()

	mclock.EXPECT().CurrentLayer().Return(epoch5.FirstLayer()).AnyTimes()
	err := pd.setBeacon(epoch3, beacon2)
	require.NoError(t, err)
	err = pd.setBeacon(epoch5, beacon4)
	require.NoError(t, err)

	got, err := pd.GetBeacon(epoch3)
	require.NoError(t, err)
	require.Equal(t, beacon2, got)

	got, err = pd.GetBeacon(epoch5)
	require.NoError(t, err)
	require.Equal(t, beacon4, got)

	got, err = pd.GetBeacon(epoch5 - 1)
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)

	// clear out the in-memory map
	// the database should still give us values
	pd.mu.Lock()
	clear(pd.beacons)
	pd.mu.Unlock()

	got, err = pd.GetBeacon(epoch3)
	require.NoError(t, err)
	require.Equal(t, beacon2, got)

	got, err = pd.GetBeacon(epoch5)
	require.NoError(t, err)
	require.Equal(t, beacon4, got)

	got, err = pd.GetBeacon(epoch5 - 1)
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeacon_BeaconsWithDatabaseFailure(t *testing.T) {
	t.Parallel()

	lg := zaptest.NewLogger(t)
	mclock := NewMocklayerClock(gomock.NewController(t))
	pd := &ProtocolDriver{
		logger:  lg.Named("Beacon"),
		beacons: make(map[types.EpochID]types.Beacon),
		cdb:     datastore.NewCachedDB(sql.InMemory(), lg),
		clock:   mclock,
	}
	epoch := types.EpochID(3)

	mclock.EXPECT().CurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
	got, errGet := pd.getPersistedBeacon(epoch)
	require.Equal(t, types.EmptyBeacon, got)
	require.ErrorIs(t, errGet, sql.ErrNotFound)
}

func TestBeacon_BeaconsCleanupOldEpoch(t *testing.T) {
	t.Parallel()

	lg := zaptest.NewLogger(t)
	mclock := NewMocklayerClock(gomock.NewController(t))
	pd := &ProtocolDriver{
		logger:         lg.Named("Beacon"),
		cdb:            datastore.NewCachedDB(sql.InMemory(), lg),
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: make(map[types.EpochID]map[types.Beacon]*beaconWeight),
		clock:          mclock,
	}

	epoch := types.EpochID(5)
	mclock.EXPECT().CurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
	for i := range numEpochsToKeep {
		e := epoch + types.EpochID(i)
		err := pd.setBeacon(e, types.RandomBeacon())
		require.NoError(t, err)
		b := types.NewExistingBallot(types.RandomBallotID(), types.EmptyEdSignature, types.EmptyNodeID, e.FirstLayer())
		b.EligibilityProofs = []types.VotingEligibility{{J: 1}}
		pd.ReportBeaconFromBallot(e, &b, types.RandomBeacon(), fixed.New64(1))
		pd.cleanupEpoch(e)
		require.Len(t, pd.beacons, i+1)
		require.Len(t, pd.ballotsBeacons, i+1)
	}
	require.Len(t, pd.beacons, numEpochsToKeep)
	require.Len(t, pd.ballotsBeacons, numEpochsToKeep)

	epoch = epoch + numEpochsToKeep
	err := pd.setBeacon(epoch, types.RandomBeacon())
	require.NoError(t, err)
	b := types.NewExistingBallot(types.RandomBallotID(), types.EmptyEdSignature, types.EmptyNodeID, epoch.FirstLayer())
	b.EligibilityProofs = []types.VotingEligibility{{J: 1}}
	pd.recordBeacon(epoch, &b, types.RandomBeacon(), fixed.New64(1))
	require.Len(t, pd.beacons, numEpochsToKeep+1)
	require.Len(t, pd.ballotsBeacons, numEpochsToKeep+1)
	pd.cleanupEpoch(epoch)
	require.Len(t, pd.beacons, numEpochsToKeep)
	require.Len(t, pd.ballotsBeacons, numEpochsToKeep)
}

func TestBeacon_ReportBeaconFromBallot(t *testing.T) {
	t.Parallel()

	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	beacon3 := types.RandomBeacon()
	tt := []struct {
		name          string
		majority      bool
		beacon        types.Beacon
		beaconBallots map[types.Beacon][]fixed.Fixed
	}{
		{
			name: "majority",
			beaconBallots: map[types.Beacon][]fixed.Fixed{
				beacon1: {fixed.New64(1), fixed.New64(1)},
				beacon2: {fixed.New64(1)},
				beacon3: {fixed.Div64(1, 10)},
			},
			majority: true,
			beacon:   beacon1,
		},
		{
			name: "plurality",
			beaconBallots: map[types.Beacon][]fixed.Fixed{
				beacon1: {fixed.New64(1)},
				beacon2: {fixed.Div64(11, 10)},
				beacon3: {fixed.Div64(3, 10), fixed.Div64(7, 10)},
			},
			beacon: beacon2,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// making sure the math in test arguments are correct
			total := fixed.New64(0)
			beaconWeights := make(map[types.Beacon]fixed.Fixed)
			for beacon, weights := range tc.beaconBallots {
				bweight := fixed.New64(0)
				for _, w := range weights {
					total = total.Add(w)
					bweight = bweight.Add(w)
				}
				beaconWeights[beacon] = bweight
			}
			maxWeight := fixed.New64(0)
			majorityWeight := total.Div(fixed.New64(2))
			var found bool
			for _, weight := range beaconWeights {
				if weight.GreaterThan(majorityWeight) {
					found = true
				}
				if weight.GreaterThan(maxWeight) {
					maxWeight = weight
				}
			}
			if tc.majority {
				require.True(t, found)
			} else {
				require.Greater(t, maxWeight.Float(), 0.0)
			}

			lg := zaptest.NewLogger(t)
			mclock := NewMocklayerClock(gomock.NewController(t))
			pd := &ProtocolDriver{
				logger:         lg.Named("Beacon"),
				config:         UnitTestConfig(),
				cdb:            datastore.NewCachedDB(sql.InMemory(), lg),
				beacons:        make(map[types.EpochID]types.Beacon),
				ballotsBeacons: make(map[types.EpochID]map[types.Beacon]*beaconWeight),
				clock:          mclock,
			}
			pd.config.BeaconSyncWeightUnits = 4

			epoch := types.EpochID(3)
			mclock.EXPECT().CurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
			for beacon, weights := range tc.beaconBallots {
				for _, w := range weights {
					b := types.NewExistingBallot(
						types.RandomBallotID(),
						types.EmptyEdSignature,
						types.EmptyNodeID,
						epoch.FirstLayer(),
					)
					b.EligibilityProofs = []types.VotingEligibility{{J: 1}}
					pd.ReportBeaconFromBallot(epoch, &b, beacon, w)
				}
			}
			got, err := pd.GetBeacon(epoch)
			require.NoError(t, err)
			require.Equal(t, tc.beacon, got)
		})
	}
}

func TestBeacon_ReportBeaconFromBallot_SameBallot(t *testing.T) {
	t.Parallel()

	lg := zaptest.NewLogger(t)
	mclock := NewMocklayerClock(gomock.NewController(t))
	pd := &ProtocolDriver{
		logger:         lg.Named("Beacon"),
		config:         UnitTestConfig(),
		cdb:            datastore.NewCachedDB(sql.InMemory(), lg),
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: make(map[types.EpochID]map[types.Beacon]*beaconWeight),
		clock:          mclock,
	}
	pd.config.BeaconSyncWeightUnits = 2

	epoch := types.EpochID(3)
	mclock.EXPECT().CurrentLayer().Return(epoch.FirstLayer())
	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()

	b1 := types.NewExistingBallot(types.RandomBallotID(), types.EmptyEdSignature, types.EmptyNodeID, epoch.FirstLayer())
	b1.EligibilityProofs = []types.VotingEligibility{{J: 1}}
	pd.ReportBeaconFromBallot(epoch, &b1, beacon1, fixed.New64(1))
	pd.ReportBeaconFromBallot(epoch, &b1, beacon1, fixed.New64(1))
	// same ballotID does not count twice
	got, err := pd.GetBeacon(epoch)
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)

	b2 := types.NewExistingBallot(types.RandomBallotID(), types.EmptyEdSignature, types.EmptyNodeID, epoch.FirstLayer())
	b2.EligibilityProofs = []types.VotingEligibility{{J: 1}}
	pd.ReportBeaconFromBallot(epoch, &b2, beacon2, fixed.New64(2))
	got, err = pd.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon2, got)
}

func TestBeacon_ensureEpochHasBeacon_BeaconAlreadyCalculated(t *testing.T) {
	t.Parallel()

	epoch := types.EpochID(3)
	beacon := types.RandomBeacon()
	beaconFromBallots := types.RandomBeacon()
	pd := &ProtocolDriver{
		logger: zaptest.NewLogger(t).Named("Beacon"),
		config: UnitTestConfig(),
		beacons: map[types.EpochID]types.Beacon{
			epoch: beacon,
		},
		ballotsBeacons: make(map[types.EpochID]map[types.Beacon]*beaconWeight),
	}
	pd.config.BeaconSyncWeightUnits = 2

	got, err := pd.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon, got)

	b1 := types.NewExistingBallot(types.RandomBallotID(), types.EmptyEdSignature, types.EmptyNodeID, epoch.FirstLayer())
	b1.EligibilityProofs = []types.VotingEligibility{{J: 1}}
	pd.ReportBeaconFromBallot(epoch, &b1, beaconFromBallots, fixed.New64(1))
	b2 := types.NewExistingBallot(types.RandomBallotID(), types.EmptyEdSignature, types.EmptyNodeID, epoch.FirstLayer())
	b2.EligibilityProofs = []types.VotingEligibility{{J: 1}}
	pd.ReportBeaconFromBallot(epoch, &b2, beaconFromBallots, fixed.New64(1))

	// should not change the beacon value
	got, err = pd.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon, got)
}

func TestBeacon_findMajorityBeacon(t *testing.T) {
	t.Parallel()

	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	beacon3 := types.RandomBeacon()

	beaconFromBallots := map[types.Beacon]*beaconWeight{
		beacon1: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}, types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(1),
			numEligibility: 2,
		},
		beacon2: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(3),
			numEligibility: 1,
		},
		beacon3: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(1),
			numEligibility: 1,
		},
	}
	epoch := types.EpochID(3)
	pd := &ProtocolDriver{
		logger:         zaptest.NewLogger(t).Named("Beacon"),
		config:         UnitTestConfig(),
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: map[types.EpochID]map[types.Beacon]*beaconWeight{epoch: beaconFromBallots},
	}
	pd.config.BeaconSyncWeightUnits = 4
	got := pd.findMajorityBeacon(epoch)
	require.Equal(t, beacon2, got)
}

func TestBeacon_findMajorityBeacon_plurality(t *testing.T) {
	t.Parallel()

	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	beacon3 := types.RandomBeacon()

	beaconFromBallots := map[types.Beacon]*beaconWeight{
		beacon1: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}, types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(1),
			numEligibility: 2,
		},
		beacon2: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			totalWeight:    fixed.DivUint64(11, 10),
			numEligibility: 1,
		},
		beacon3: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(1),
			numEligibility: 1,
		},
	}
	epoch := types.EpochID(3)
	pd := &ProtocolDriver{
		logger:         zaptest.NewLogger(t).Named("Beacon"),
		config:         UnitTestConfig(),
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: map[types.EpochID]map[types.Beacon]*beaconWeight{epoch: beaconFromBallots},
	}
	pd.config.BeaconSyncWeightUnits = 4
	got := pd.findMajorityBeacon(epoch)
	require.Equal(t, beacon2, got)
}

func TestBeacon_findMajorityBeacon_NotEnoughBallots(t *testing.T) {
	t.Parallel()

	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	beacon3 := types.RandomBeacon()

	beaconFromBallots := map[types.Beacon]*beaconWeight{
		beacon1: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}, types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(1),
			numEligibility: 2,
		},
		beacon2: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(3),
			numEligibility: 1,
		},
		beacon3: {
			ballots:        map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			totalWeight:    fixed.New64(1),
			numEligibility: 1,
		},
	}
	epoch := types.EpochID(3)
	pd := &ProtocolDriver{
		logger:         zaptest.NewLogger(t).Named("Beacon"),
		config:         UnitTestConfig(),
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: map[types.EpochID]map[types.Beacon]*beaconWeight{epoch: beaconFromBallots},
	}
	pd.config.BeaconSyncWeightUnits = 5
	got := pd.findMajorityBeacon(epoch)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeacon_findMajorityBeacon_NoBeacon(t *testing.T) {
	t.Parallel()

	pd := &ProtocolDriver{
		logger:         zaptest.NewLogger(t).Named("Beacon"),
		config:         UnitTestConfig(),
		beacons:        make(map[types.EpochID]types.Beacon),
		ballotsBeacons: make(map[types.EpochID]map[types.Beacon]*beaconWeight),
	}
	epoch := types.EpochID(3)
	got := pd.findMajorityBeacon(epoch)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeacon_setBeacon(t *testing.T) {
	t.Parallel()

	tpd := setUpProtocolDriver(t)
	epoch := types.EpochID(5)
	tpd.mClock.EXPECT().CurrentLayer().Return(epoch.FirstLayer()).AnyTimes()
	beacon := types.RandomBeacon()
	require.NoError(t, tpd.setBeacon(epoch, beacon))

	// saving it again won't cause error
	require.NoError(t, tpd.setBeacon(epoch, beacon))
	// but saving a different one will
	require.ErrorIs(t, tpd.setBeacon(epoch, types.RandomBeacon()), errDifferentBeacon)
}

func TestBeacon_atxThresholdFraction(t *testing.T) {
	t.Parallel()

	kappa := 40
	q := big.NewRat(1, 3)
	tt := []struct {
		name      string
		w         int
		threshold string
	}{
		{
			name:      "0 atxs",
			w:         0,
			threshold: "0",
		},
		{
			name:      "30 atxs",
			w:         30,
			threshold: "0.75",
		},
		{
			name:      "10000 atxs",
			w:         10000,
			threshold: "0.004150246906",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			threshold := atxThresholdFraction(kappa, q, tc.w)
			expected, ok := new(big.Float).SetString(tc.threshold)
			require.True(t, ok)
			require.Equal(t, expected.String(), threshold.String())
		})
	}
}

func TestBeacon_atxThreshold(t *testing.T) {
	t.Parallel()

	kappa := 40
	q := big.NewRat(1, 3)
	//nolint:lll
	tt := []struct {
		name      string
		w         int
		threshold string
	}{
		{
			name: "Case 1",
			w:    60,
			threshold: "22812203088110976093205858028501456624466142536242799652895962589496375836043386932529564056586" +
				"85699889321154786797203655344352360687718999126330659861107094125997337180132475041437096123301888",
		},
		{
			name: "Case 2",
			w:    10_000,
			threshold: "18935255055005106377502632398712282551719911452308460382048488311892953261334543784347262759720" +
				"917437038480763542475179136852475093285227949507665240470812709909012324673914264266057279602688",
		},
		{
			name: "Case 3",
			w:    100_000,
			threshold: "18970711981368999716491430411587745105506104524672542810046809328543990110060906572290051896861" +
				"41691549362111655061764931498490692502427712970984851624895957066181188995976870137803158061056",
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			threshold := atxThreshold(kappa, q, tc.w)
			expected, ok := new(big.Int).SetString(tc.threshold, 10)
			require.True(t, ok)
			require.Equal(t, expected, threshold)
		})
	}
}

func TestBeacon_proposalPassesEligibilityThreshold(t *testing.T) {
	cfg := Config{Kappa: 40, Q: *big.NewRat(1, 3)}
	tt := []struct {
		name            string
		wEarly, wOntime int
	}{
		{
			name:    "30 atxs",
			wEarly:  27,
			wOntime: 30,
		},
		{
			name:    "1K atxs",
			wEarly:  900,
			wOntime: 1_000,
		},
		{
			name:    "10K atxs",
			wEarly:  9_000,
			wOntime: 10_000,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger := zaptest.NewLogger(t)
			checker := createProposalChecker(logger.Named("proposal checker"), cfg, tc.wEarly, tc.wOntime)
			var numEligible, numEligibleStrict int
			for i := 0; i < tc.wEarly; i++ {
				signer, err := signing.NewEdSigner()
				require.NoError(t, err)
				proposal := buildSignedProposal(
					context.Background(),
					logger,
					signer.VRFSigner(),
					3,
					types.VRFPostIndex(1),
				)
				if checker.PassThreshold(proposal) {
					numEligible++
				}
				if checker.PassStrictThreshold(proposal) {
					numEligibleStrict++
				}
			}
			require.NotZero(t, numEligible)
			require.NotZero(t, numEligibleStrict)
			require.LessOrEqual(t, numEligibleStrict, numEligible)
		})
	}
}

func TestBeacon_buildProposal(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name   string
		epoch  types.EpochID
		result string
	}{
		{
			name:   "Case 1",
			epoch:  13110,
			result: string([]byte{0x04, 0x04, 0xd9, 0xcc}),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := buildProposal(tc.epoch, types.VRFPostIndex(1))
			require.Equal(t, tc.result, string(result))
		})
	}
}

func TestBeacon_getSignedProposal(t *testing.T) {
	t.Parallel()

	edSgn, err := signing.NewEdSigner()
	require.NoError(t, err)

	tt := []struct {
		name   string
		epoch  types.EpochID
		result types.VrfSignature
	}{
		{
			name:   "Case 1",
			epoch:  1,
			result: edSgn.VRFSigner().Sign([]byte{0x04, 0x04, 0x04}),
		},
		{
			name:   "Case 2",
			epoch:  2,
			result: edSgn.VRFSigner().Sign([]byte{0x04, 0x04, 0x08}),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := buildSignedProposal(
				context.Background(),
				zaptest.NewLogger(t),
				edSgn.VRFSigner(),
				tc.epoch,
				types.VRFPostIndex(1),
			)
			require.Equal(t, tc.result, result)
		})
	}
}

func TestBeacon_calcBeacon(t *testing.T) {
	set := proposalSet{
		Proposal{0x01}: {},
		Proposal{0x02}: {},
		Proposal{0x04}: {},
		Proposal{0x05}: {},
	}

	beacon := calcBeacon(zaptest.NewLogger(t), set)
	expected := types.HexToBeacon("0xe69fd154")
	require.EqualValues(t, expected, beacon)
}
