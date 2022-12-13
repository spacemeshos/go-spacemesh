package beacon

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/beacon/mocks"
	"github.com/spacemeshos/go-spacemesh/beacon/weakcoin"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	pubsubmocks "github.com/spacemeshos/go-spacemesh/p2p/pubsub/mocks"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	smocks "github.com/spacemeshos/go-spacemesh/system/mocks"
)

const (
	numATXs = 10
)

func coinValueMock(tb testing.TB, value bool) coin {
	ctrl := gomock.NewController(tb)
	coinMock := mocks.NewMockcoin(ctrl)
	coinMock.EXPECT().StartEpoch(
		gomock.Any(),
		gomock.AssignableToTypeOf(types.EpochID(0)),
		gomock.AssignableToTypeOf(weakcoin.UnitAllowances{}),
	).AnyTimes()
	coinMock.EXPECT().FinishEpoch(gomock.Any(), gomock.AssignableToTypeOf(types.EpochID(0))).AnyTimes()
	coinMock.EXPECT().StartRound(gomock.Any(), gomock.AssignableToTypeOf(types.RoundID(0))).
		AnyTimes().Return(nil)
	coinMock.EXPECT().FinishRound(gomock.Any()).AnyTimes()
	coinMock.EXPECT().Get(
		gomock.Any(),
		gomock.AssignableToTypeOf(types.EpochID(0)),
		gomock.AssignableToTypeOf(types.RoundID(0)),
	).AnyTimes().Return(value)
	return coinMock
}

func newPublisher(tb testing.TB) pubsub.Publisher {
	tb.Helper()
	ctrl := gomock.NewController(tb)
	defer ctrl.Finish()

	publisher := pubsubmocks.NewMockPublisher(ctrl)
	publisher.EXPECT().
		Publish(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil).
		AnyTimes()
	return publisher
}

type testProtocolDriver struct {
	*ProtocolDriver
	cdb    *datastore.CachedDB
	mClock *mocks.MocklayerClock
	mSync  *smocks.MockSyncStateProvider
}

func setUpProtocolDriver(t *testing.T) *testProtocolDriver {
	return newTestDriver(t, UnitTestConfig(), newPublisher(t))
}

func newTestDriver(t *testing.T, cfg Config, p pubsub.Publisher) *testProtocolDriver {
	ctrl := gomock.NewController(t)
	tpd := &testProtocolDriver{
		mClock: mocks.NewMocklayerClock(ctrl),
		mSync:  smocks.NewMockSyncStateProvider(ctrl),
	}
	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()
	vrfSigner := edSgn.VRFSigner()
	minerID := types.BytesToNodeID(edPubkey.Bytes())
	lg := logtest.New(t).WithName(minerID.ShortString())
	tpd.cdb = datastore.NewCachedDB(sql.InMemory(), lg)
	tpd.ProtocolDriver = New(minerID, edSgn, vrfSigner, tpd.cdb, tpd.mClock,
		WithConfig(cfg),
		WithPublisher(p),
		WithLogger(lg),
		withWeakCoin(coinValueMock(t, true)),
		withCheckerFunc(func(_ log.Log, _ Config, _ int) eligibilityChecker {
			mc := mocks.NewMockeligibilityChecker(gomock.NewController(t))
			mc.EXPECT().IsProposalEligible(gomock.Any()).Return(true).AnyTimes()
			return mc
		}))
	tpd.ProtocolDriver.SetSyncState(tpd.mSync)
	tpd.ProtocolDriver.setMetricsRegistry(prometheus.NewPedanticRegistry())
	return tpd
}

func createATX(t *testing.T, db *datastore.CachedDB, lid types.LayerID, sig *signing.EdSigner, numUnits uint32) {
	atx := types.NewActivationTx(
		types.NIPostChallenge{PubLayerID: lid},
		types.Address{},
		nil,
		numUnits,
		nil,
	)

	require.NoError(t, activation.SignAtx(sig, atx))
	vAtx, err := atx.Verify(0, 1)
	require.NoError(t, err)
	require.NoError(t, atxs.Add(db, vAtx, time.Now().Add(-1*time.Second)))
}

func createRandomATXs(t *testing.T, db *datastore.CachedDB, lid types.LayerID, num int) {
	for i := 0; i < num; i++ {
		createATX(t, db, lid, signing.NewEdSigner(), 1)
	}
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func TestBeacon_MultipleNodes(t *testing.T) {
	numNodes := 5
	testNodes := make([]*testProtocolDriver, 0, numNodes)
	publisher := pubsubmocks.NewMockPublisher(gomock.NewController(t))
	publisher.EXPECT().Publish(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, protocol string, data []byte) error {
			for _, node := range testNodes {
				switch protocol {
				case pubsub.BeaconProposalProtocol:
					require.NoError(t, node.handleProposal(ctx, p2p.Peer(node.nodeID.ShortString()), data, time.Now()))
				case pubsub.BeaconFirstVotesProtocol:
					require.NoError(t, node.handleFirstVotes(ctx, p2p.Peer(node.nodeID.ShortString()), data))
				case pubsub.BeaconFollowingVotesProtocol:
					require.NoError(t, node.handleFollowingVotes(ctx, p2p.Peer(node.nodeID.ShortString()), data, time.Now()))
				case pubsub.BeaconWeakCoinProtocol:
				}
			}
			return nil
		}).AnyTimes()

	atxPublishLid := types.NewLayerID(types.GetLayersPerEpoch()*2 - 1)
	current := atxPublishLid.Add(1)
	dbs := make([]*datastore.CachedDB, 0, numNodes)
	cfg := NodeSimUnitTestConfig()
	now := time.Now()
	for i := 0; i < numNodes; i++ {
		node := newTestDriver(t, cfg, publisher)
		node.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
		node.mClock.EXPECT().GetCurrentLayer().Return(current).AnyTimes()
		node.mClock.EXPECT().LayerToTime(current).Return(now).AnyTimes()
		testNodes = append(testNodes, node)
		dbs = append(dbs, node.cdb)

		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(0)), errGenesis)
		require.ErrorIs(t, node.onNewEpoch(context.Background(), types.EpochID(1)), errGenesis)
		got, err := node.GetBeacon(types.EpochID(1))
		require.NoError(t, err)
		require.EqualValues(t, got, types.HexToBeacon(types.BootstrapBeacon))
		got, err = node.GetBeacon(types.EpochID(2))
		require.NoError(t, err)
		require.EqualValues(t, got, types.HexToBeacon(types.BootstrapBeacon))
	}

	var wg sync.WaitGroup
	for _, node := range testNodes {
		for _, db := range dbs {
			createATX(t, db, atxPublishLid, node.edSigner, 1)
		}
		wg.Add(1)
		go func(testNode *testProtocolDriver) {
			require.NoError(t, testNode.onNewEpoch(context.TODO(), types.EpochID(2)))
			wg.Done()
		}(node)
	}
	wg.Wait()
	beacons := make(map[types.Beacon]struct{})
	for _, node := range testNodes {
		got, err := node.GetBeacon(types.EpochID(3))
		require.NoError(t, err)
		require.NotEqual(t, types.EmptyBeacon, got)
		beacons[got] = struct{}{}
	}
	require.Len(t, beacons, 1)
}

func TestBeaconNotSynced(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	require.ErrorIs(t, tpd.onNewEpoch(context.TODO(), types.EpochID(0)), errGenesis)
	require.ErrorIs(t, tpd.onNewEpoch(context.TODO(), types.EpochID(1)), errGenesis)
	require.ErrorIs(t, tpd.onNewEpoch(context.TODO(), types.EpochID(2)), errNodeNotSynced)

	got, err := tpd.GetBeacon(types.EpochID(2))
	require.NoError(t, err)
	require.EqualValues(t, got, types.HexToBeacon(types.BootstrapBeacon))
	got, err = tpd.GetBeacon(types.EpochID(3))
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeaconNotSynced_ReleaseMemory(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.config.BeaconSyncNumBallots = 1
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()
	start := types.EpochID(2)
	end := start + numEpochsToKeep + 10
	for eid := start; eid <= end; eid++ {
		tpd.ReportBeaconFromBallot(eid, types.RandomBallotID(), types.RandomBeacon(), 1000)
		require.ErrorIs(t, tpd.onNewEpoch(context.TODO(), eid), errNodeNotSynced)
	}
	require.Len(t, tpd.beacons, numEpochsToKeep)
	require.Len(t, tpd.beaconsFromBallots, numEpochsToKeep)
}

func TestBeaconNoATXInPreviousEpoch(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()
	require.ErrorIs(t, tpd.onNewEpoch(context.TODO(), types.EpochID(0)), errGenesis)
	require.ErrorIs(t, tpd.onNewEpoch(context.TODO(), types.EpochID(1)), errGenesis)
	lid := types.NewLayerID(types.GetLayersPerEpoch()*2 - 1)
	createRandomATXs(t, tpd.cdb, lid, numATXs)
	require.ErrorIs(t, tpd.onNewEpoch(context.TODO(), types.EpochID(2)), sql.ErrNotFound)

	got, err := tpd.GetBeacon(types.EpochID(2))
	require.NoError(t, err)
	require.EqualValues(t, got, types.HexToBeacon(types.BootstrapBeacon))
	got, err = tpd.GetBeacon(types.EpochID(3))
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeaconWithMetrics(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()

	gLayer := types.GetEffectiveGenesis()
	tpd.mClock.EXPECT().Subscribe().Times(1)
	tpd.mClock.EXPECT().GetCurrentLayer().Return(gLayer).Times(1)
	tpd.mClock.EXPECT().LayerToTime((gLayer.GetEpoch() + 1).FirstLayer()).Return(time.Now()).Times(1)
	tpd.Start(context.TODO())

	epoch3Beacon := types.HexToBeacon("0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	epoch := types.EpochID(3)
	for i := types.EpochID(2); i < epoch; i++ {
		lid := i.FirstLayer().Sub(1)
		createATX(t, tpd.cdb, lid, tpd.edSigner, 199)
		createRandomATXs(t, tpd.cdb, lid, numATXs-1)
	}
	finalLayer := types.NewLayerID(types.GetLayersPerEpoch() * uint32(epoch))
	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	for layer := gLayer.Add(1); layer.Before(finalLayer); layer = layer.Add(1) {
		tpd.mClock.EXPECT().GetCurrentLayer().Return(layer).AnyTimes()
		if layer.FirstInEpoch() {
			require.NoError(t, tpd.onNewEpoch(context.TODO(), layer.GetEpoch()))
		}
		thisEpoch := layer.GetEpoch()
		tpd.recordBeacon(thisEpoch, types.RandomBallotID(), beacon1, 100)
		tpd.recordBeacon(thisEpoch, types.RandomBallotID(), beacon2, 200)

		numCalculated := 0
		numObserved := 0
		numObservedWeight := 0
		allMetrics, err := prometheus.DefaultGatherer.Gather()
		require.NoError(t, err)
		for _, m := range allMetrics {
			switch *m.Name {
			case "spacemesh_beacons_beacon_calculated_weight":
				require.Equal(t, 1, len(m.Metric))
				numCalculated++
				beaconStr := epoch3Beacon.ShortString()
				expected := fmt.Sprintf("label:<name:\"beacon\" value:\"%s\" > label:<name:\"epoch\" value:\"%d\" > counter:<value:%d > ", beaconStr, thisEpoch+1, 199)
				require.Equal(t, expected, m.Metric[0].String())
			case "spacemesh_beacons_beacon_observed_total":
				require.Equal(t, 2, len(m.Metric))
				numObserved = numObserved + 2
				count := layer.OrdinalInEpoch() + 1
				expected := []string{
					fmt.Sprintf("label:<name:\"beacon\" value:\"%s\" > label:<name:\"epoch\" value:\"%d\" > counter:<value:%d > ", beacon1.ShortString(), thisEpoch, count),
					fmt.Sprintf("label:<name:\"beacon\" value:\"%s\" > label:<name:\"epoch\" value:\"%d\" > counter:<value:%d > ", beacon2.ShortString(), thisEpoch, count),
				}
				for _, subM := range m.Metric {
					require.Contains(t, expected, subM.String())
				}
			case "spacemesh_beacons_beacon_observed_weight":
				require.Equal(t, 2, len(m.Metric))
				numObservedWeight = numObservedWeight + 2
				weight := (layer.OrdinalInEpoch() + 1) * 100
				expected := []string{
					fmt.Sprintf("label:<name:\"beacon\" value:\"%s\" > label:<name:\"epoch\" value:\"%d\" > counter:<value:%d > ", beacon1.ShortString(), thisEpoch, weight),
					fmt.Sprintf("label:<name:\"beacon\" value:\"%s\" > label:<name:\"epoch\" value:\"%d\" > counter:<value:%d > ", beacon2.ShortString(), thisEpoch, weight*2),
				}
				for _, subM := range m.Metric {
					require.Contains(t, expected, subM.String())
				}
			}
		}
		if layer.OrdinalInEpoch() == 2 {
			// there should be calculated beacon already by the last layer of the epoch
			require.Equal(t, 1, numCalculated, layer)
		} else {
			require.LessOrEqual(t, numCalculated, 1, layer)
		}
		require.Equal(t, 2, numObserved, layer)
		require.Equal(t, 2, numObservedWeight, layer)
	}

	tpd.mClock.EXPECT().Unsubscribe(gomock.Any()).Times(1)
	tpd.Close()
}

func TestBeacon_BeaconsWithDatabase(t *testing.T) {
	t.Parallel()

	pd := &ProtocolDriver{
		logger:  logtest.New(t).WithName("Beacon"),
		beacons: make(map[types.EpochID]types.Beacon),
		cdb:     datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
	}
	epoch3 := types.EpochID(3)
	beacon2 := types.RandomBeacon()
	epoch5 := types.EpochID(5)
	beacon4 := types.RandomBeacon()
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
	pd.beacons = make(map[types.EpochID]types.Beacon)
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

	pd := &ProtocolDriver{
		logger:  logtest.New(t).WithName("Beacon"),
		beacons: make(map[types.EpochID]types.Beacon),
		cdb:     datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
	}
	epoch := types.EpochID(3)

	got, errGet := pd.getPersistedBeacon(epoch)
	require.Equal(t, types.EmptyBeacon, got)
	require.ErrorIs(t, errGet, sql.ErrNotFound)
}

func TestBeacon_BeaconsCleanupOldEpoch(t *testing.T) {
	t.Parallel()

	pd := &ProtocolDriver{
		logger:             logtest.New(t).WithName("Beacon"),
		cdb:                datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
		beacons:            make(map[types.EpochID]types.Beacon),
		beaconsFromBallots: make(map[types.EpochID]map[types.Beacon]*ballotWeight),
	}

	epoch := types.EpochID(5)
	for i := 0; i < numEpochsToKeep; i++ {
		e := epoch + types.EpochID(i)
		err := pd.setBeacon(e, types.RandomBeacon())
		require.NoError(t, err)
		pd.recordBeacon(e, types.RandomBallotID(), types.RandomBeacon(), 10)
		pd.cleanupEpoch(e)
		require.Equal(t, i+1, len(pd.beacons))
		require.Equal(t, i+1, len(pd.beaconsFromBallots))
	}
	require.Equal(t, numEpochsToKeep, len(pd.beacons))
	require.Equal(t, numEpochsToKeep, len(pd.beaconsFromBallots))

	epoch = epoch + numEpochsToKeep
	err := pd.setBeacon(epoch, types.RandomBeacon())
	require.NoError(t, err)
	pd.recordBeacon(epoch, types.RandomBallotID(), types.RandomBeacon(), 10)
	require.Equal(t, numEpochsToKeep+1, len(pd.beacons))
	require.Equal(t, numEpochsToKeep+1, len(pd.beaconsFromBallots))
	pd.cleanupEpoch(epoch)
	require.Equal(t, numEpochsToKeep, len(pd.beacons))
	require.Equal(t, numEpochsToKeep, len(pd.beaconsFromBallots))
}

func TestBeacon_ReportBeaconFromBallot(t *testing.T) {
	t.Parallel()

	pd := &ProtocolDriver{
		logger:             logtest.New(t).WithName("Beacon"),
		config:             UnitTestConfig(),
		cdb:                datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
		beacons:            make(map[types.EpochID]types.Beacon),
		beaconsFromBallots: make(map[types.EpochID]map[types.Beacon]*ballotWeight),
	}
	pd.config.BeaconSyncNumBallots = 3

	epoch := types.EpochID(3)
	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	pd.ReportBeaconFromBallot(epoch, types.RandomBallotID(), beacon1, 100)
	got, err := pd.GetBeacon(epoch)
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)
	pd.ReportBeaconFromBallot(epoch, types.RandomBallotID(), beacon2, 100)
	got, err = pd.GetBeacon(epoch)
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)
	pd.ReportBeaconFromBallot(epoch, types.RandomBallotID(), beacon1, 1)
	got, err = pd.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon1, got)
}

func TestBeacon_ReportBeaconFromBallot_SameBallot(t *testing.T) {
	t.Parallel()

	pd := &ProtocolDriver{
		logger:             logtest.New(t).WithName("Beacon"),
		config:             UnitTestConfig(),
		cdb:                datastore.NewCachedDB(sql.InMemory(), logtest.New(t)),
		beacons:            make(map[types.EpochID]types.Beacon),
		beaconsFromBallots: make(map[types.EpochID]map[types.Beacon]*ballotWeight),
	}
	pd.config.BeaconSyncNumBallots = 2

	epoch := types.EpochID(3)
	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	ballotID1 := types.RandomBallotID()
	ballotID2 := types.RandomBallotID()
	pd.ReportBeaconFromBallot(epoch, ballotID1, beacon1, 100)
	pd.ReportBeaconFromBallot(epoch, ballotID1, beacon1, 200)
	// same ballotID does not count twice
	got, err := pd.GetBeacon(epoch)
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)

	pd.ReportBeaconFromBallot(epoch, ballotID2, beacon2, 101)
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
		logger: logtest.New(t).WithName("Beacon"),
		config: UnitTestConfig(),
		beacons: map[types.EpochID]types.Beacon{
			epoch: beacon,
		},
		beaconsFromBallots: make(map[types.EpochID]map[types.Beacon]*ballotWeight),
	}
	pd.config.BeaconSyncNumBallots = 2

	got, err := pd.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon, got)

	pd.ReportBeaconFromBallot(epoch, types.RandomBallotID(), beaconFromBallots, 100)
	pd.ReportBeaconFromBallot(epoch, types.RandomBallotID(), beaconFromBallots, 200)

	// should not change the beacon value
	got, err = pd.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon, got)
}

func TestBeacon_findMostWeightedBeaconForEpoch(t *testing.T) {
	t.Parallel()

	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	beacon3 := types.RandomBeacon()

	beaconsFromBlocks := map[types.Beacon]*ballotWeight{
		beacon1: {
			ballots: map[types.BallotID]struct{}{types.RandomBallotID(): {}, types.RandomBallotID(): {}},
			weight:  200,
		},
		beacon2: {
			ballots: map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			weight:  201,
		},
		beacon3: {
			ballots: map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			weight:  200,
		},
	}
	epoch := types.EpochID(3)
	pd := &ProtocolDriver{
		logger:             logtest.New(t).WithName("Beacon"),
		config:             UnitTestConfig(),
		beacons:            make(map[types.EpochID]types.Beacon),
		beaconsFromBallots: map[types.EpochID]map[types.Beacon]*ballotWeight{epoch: beaconsFromBlocks},
	}
	pd.config.BeaconSyncNumBallots = 2
	got := pd.findMostWeightedBeaconForEpoch(epoch)
	require.Equal(t, beacon2, got)
}

func TestBeacon_findMostWeightedBeaconForEpoch_NotEnoughBallots(t *testing.T) {
	t.Parallel()

	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	beacon3 := types.RandomBeacon()

	beaconsFromBlocks := map[types.Beacon]*ballotWeight{
		beacon1: {
			ballots: map[types.BallotID]struct{}{types.RandomBallotID(): {}, types.RandomBallotID(): {}},
			weight:  200,
		},
		beacon2: {
			ballots: map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			weight:  201,
		},
		beacon3: {
			ballots: map[types.BallotID]struct{}{types.RandomBallotID(): {}},
			weight:  200,
		},
	}
	epoch := types.EpochID(3)
	pd := &ProtocolDriver{
		logger:             logtest.New(t).WithName("Beacon"),
		config:             UnitTestConfig(),
		beacons:            make(map[types.EpochID]types.Beacon),
		beaconsFromBallots: map[types.EpochID]map[types.Beacon]*ballotWeight{epoch: beaconsFromBlocks},
	}
	pd.config.BeaconSyncNumBallots = 5
	got := pd.findMostWeightedBeaconForEpoch(epoch)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeacon_findMostWeightedBeaconForEpoch_NoBeacon(t *testing.T) {
	t.Parallel()

	pd := &ProtocolDriver{
		logger:             logtest.New(t).WithName("Beacon"),
		config:             UnitTestConfig(),
		beacons:            make(map[types.EpochID]types.Beacon),
		beaconsFromBallots: make(map[types.EpochID]map[types.Beacon]*ballotWeight),
	}
	epoch := types.EpochID(3)
	got := pd.findMostWeightedBeaconForEpoch(epoch)
	require.Equal(t, types.EmptyBeacon, got)
}

func TestBeacon_persistBeacon(t *testing.T) {
	t.Parallel()

	tpd := setUpProtocolDriver(t)
	epoch := types.EpochID(5)
	beacon := types.RandomBeacon()
	require.NoError(t, tpd.setBeacon(epoch, beacon))

	// saving it again won't cause error
	require.NoError(t, tpd.persistBeacon(epoch, beacon))
	// but saving a different one will
	require.ErrorIs(t, tpd.persistBeacon(epoch, types.RandomBeacon()), errDifferentBeacon)
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
		tc := tc
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
	tt := []struct {
		name      string
		w         int
		threshold string
	}{
		{
			name:      "Case 1",
			w:         60,
			threshold: "2281220308811097609320585802850145662446614253624279965289596258949637583604338693252956405658685699889321154786797203655344352360687718999126330659861107094125997337180132475041437096123301888",
		},
		{
			name:      "Case 2",
			w:         10_000,
			threshold: "18935255055005106377502632398712282551719911452308460382048488311892953261334543784347262759720917437038480763542475179136852475093285227949507665240470812709909012324673914264266057279602688",
		},
		{
			name:      "Case 3",
			w:         100_000,
			threshold: "1897071198136899971649143041158774510550610452467254281004680932854399011006090657229005189686141691549362111655061764931498490692502427712970984851624895957066181188995976870137803158061056",
		},
	}

	for _, tc := range tt {
		tc := tc
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
	cfg := Config{Kappa: 40, Q: big.NewRat(1, 3)}
	tt := []struct {
		name string
		w    int
	}{
		{
			name: "100K atxs",
			w:    100_000,
		},
		{
			name: "10K atxs",
			w:    10_000,
		},
		{
			name: "30 atxs",
			w:    30,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger := logtest.New(t).WithName("proposal checker")
			checker := createProposalChecker(logger, cfg, tc.w)
			numEligible := 0
			for i := 0; i < tc.w; i++ {
				vrfSigner := signing.NewEdSigner().VRFSigner()
				proposal := buildSignedProposal(context.Background(), vrfSigner, 3, logtest.New(t))
				if checker.IsProposalEligible(proposal) {
					numEligible++
				}
			}
			require.NotZero(t, numEligible)
		})
	}
}

func TestBeacon_buildProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name   string
		epoch  types.EpochID
		result string
	}{
		{
			name:   "Case 1",
			epoch:  0x12345678,
			result: string(util.Hex2Bytes("084250e259d148")),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := buildProposal(tc.epoch, logtest.New(t))
			r.Equal(tc.result, string(result))
		})
	}
}

func TestBeacon_getSignedProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	edSgn := signing.NewEdSigner()
	vrfSigner := edSgn.VRFSigner()

	tt := []struct {
		name   string
		epoch  types.EpochID
		result []byte
	}{
		{
			name:   "Case 1",
			epoch:  1,
			result: vrfSigner.Sign(util.Hex2Bytes("08425004")),
		},
		{
			name:   "Case 2",
			epoch:  2,
			result: vrfSigner.Sign(util.Hex2Bytes("08425008")),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := buildSignedProposal(context.TODO(), vrfSigner, tc.epoch, logtest.New(t))
			r.Equal(string(tc.result), string(result))
		})
	}
}

func TestBeacon_signAndExtractED(t *testing.T) {
	r := require.New(t)

	signer := signing.NewEdSigner()
	verifier := signing.NewEDVerifier()

	message := []byte{1, 2, 3, 4}

	signature := signer.Sign(message)
	extractedPK, err := verifier.Extract(message, signature)
	r.NoError(err)

	r.Equal(signer.PublicKey().String(), extractedPK.String())
}

func TestBeacon_signAndVerifyVRF(t *testing.T) {
	r := require.New(t)

	signer := signing.NewEdSigner().VRFSigner()

	verifier := signing.VRFVerifier{}

	message := []byte{1, 2, 3, 4}

	signature := signer.Sign(message)
	ok := verifier.Verify(signer.PublicKey(), message, signature)
	r.True(ok)
}

func TestBeacon_calcBeacon(t *testing.T) {
	votes := allVotes{
		support: proposalSet{
			"0x1": {},
			"0x2": {},
			"0x4": {},
			"0x5": {},
		},
		against: proposalSet{
			"0x3": {},
			"0x6": {},
		},
	}
	beacon := calcBeacon(logtest.New(t), votes)
	expected := types.HexToBeacon("0x6d148de54cc5ac334cdf4537018209b0e9f5ea94c049417103065eac777ddb5c")
	require.EqualValues(t, expected, beacon)
}
