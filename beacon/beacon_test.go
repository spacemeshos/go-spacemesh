package beacon

import (
	"context"
	"fmt"
	"math/big"
	"os"
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
	"github.com/spacemeshos/go-spacemesh/log/logtest"
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
	store  *datastore.CachedDB
	mClock *mocks.MocklayerClock
	mSync  *smocks.MockSyncStateProvider
}

func setUpProtocolDriver(t *testing.T) *testProtocolDriver {
	ctrl := gomock.NewController(t)
	tpd := &testProtocolDriver{
		mClock: mocks.NewMocklayerClock(ctrl),
		mSync:  smocks.NewMockSyncStateProvider(ctrl),
	}
	edSgn := signing.NewEdSigner([20]byte{})
	edPubkey := edSgn.PublicKey()
	vrfSigner := edSgn.VRFSigner()
	minerID := types.BytesToNodeID(edPubkey.Bytes())
	tpd.store = datastore.NewCachedDB(sql.InMemory(), logtest.New(t))
	tpd.ProtocolDriver = New(minerID, newPublisher(t), edSgn, vrfSigner, tpd.store, tpd.mClock,
		WithConfig(UnitTestConfig()),
		WithLogger(logtest.New(t).WithName("Beacon")),
		withWeakCoin(coinValueMock(t, true)))
	tpd.ProtocolDriver.SetSyncState(tpd.mSync)
	tpd.ProtocolDriver.setMetricsRegistry(prometheus.NewPedanticRegistry())
	return tpd
}

func createATX(t *testing.T, db *datastore.CachedDB, lid types.LayerID, sig *signing.EdSigner, numUnits uint) {
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
		createATX(t, db, lid, signing.NewEdSigner([20]byte{}), 1)
	}
}

func TestMain(m *testing.M) {
	types.SetLayersPerEpoch(3)

	res := m.Run()
	os.Exit(res)
}

func TestBeacon(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()

	tpd.mClock.EXPECT().Subscribe().Times(1)
	tpd.mClock.EXPECT().GetCurrentLayer().Return(types.NewLayerID(1)).Times(1)
	tpd.mClock.EXPECT().LayerToTime(types.EpochID(1).FirstLayer()).Return(time.Now()).Times(1)
	tpd.Start(context.TODO())

	tpd.onNewEpoch(context.TODO(), types.EpochID(0))
	tpd.onNewEpoch(context.TODO(), types.EpochID(1))
	lid := types.NewLayerID(types.GetLayersPerEpoch()*2 - 1)
	createATX(t, tpd.store, lid, tpd.edSigner, 1)
	createRandomATXs(t, tpd.store, lid, numATXs-1)
	tpd.onNewEpoch(context.TODO(), types.EpochID(2))

	got, err := tpd.GetBeacon(types.EpochID(2))
	require.NoError(t, err)
	require.EqualValues(t, got, types.HexToBeacon(types.BootstrapBeacon))

	got, err = tpd.GetBeacon(types.EpochID(3))
	require.NoError(t, err)
	expected := types.HexToBeacon("0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
	require.EqualValues(t, expected, got)

	tpd.mClock.EXPECT().Unsubscribe(gomock.Any()).Times(1)
	tpd.Close()
}

func TestBeaconNotSynced(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()

	tpd.mClock.EXPECT().Subscribe().Times(1)
	tpd.mClock.EXPECT().GetCurrentLayer().Return(types.NewLayerID(1)).Times(1)
	tpd.mClock.EXPECT().LayerToTime(types.EpochID(1).FirstLayer()).Return(time.Now()).Times(1)
	tpd.Start(context.TODO())

	tpd.onNewEpoch(context.TODO(), types.EpochID(0))
	tpd.onNewEpoch(context.TODO(), types.EpochID(1))
	tpd.onNewEpoch(context.TODO(), types.EpochID(2))

	got, err := tpd.GetBeacon(types.EpochID(2))
	require.NoError(t, err)
	require.EqualValues(t, got, types.HexToBeacon(types.BootstrapBeacon))

	got, err = tpd.GetBeacon(types.EpochID(3))
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)

	tpd.mClock.EXPECT().Unsubscribe(gomock.Any()).Times(1)
	tpd.Close()
}

func TestBeaconNotSynced_ReleaseMemory(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.config.BeaconSyncNumBallots = 1
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(false).AnyTimes()

	tpd.mClock.EXPECT().Subscribe().Times(1)
	tpd.mClock.EXPECT().GetCurrentLayer().Return(types.NewLayerID(1)).Times(1)
	tpd.mClock.EXPECT().LayerToTime(types.EpochID(1).FirstLayer()).Return(time.Now()).Times(1)
	tpd.Start(context.TODO())

	start := types.EpochID(2)
	end := start + numEpochsToKeep + 10
	for eid := start; eid <= end; eid++ {
		tpd.ReportBeaconFromBallot(eid, types.RandomBallotID(), types.RandomBeacon(), 1000)
		tpd.onNewEpoch(context.TODO(), eid)
	}
	require.Len(t, tpd.beacons, numEpochsToKeep)
	require.Len(t, tpd.beaconsFromBallots, numEpochsToKeep)

	tpd.mClock.EXPECT().Unsubscribe(gomock.Any()).Times(1)
	tpd.Close()
}

func TestBeaconNoATXInPreviousEpoch(t *testing.T) {
	tpd := setUpProtocolDriver(t)
	tpd.mSync.EXPECT().IsSynced(gomock.Any()).Return(true).AnyTimes()

	tpd.mClock.EXPECT().Subscribe().Times(1)
	tpd.mClock.EXPECT().GetCurrentLayer().Return(types.NewLayerID(1)).Times(1)
	tpd.mClock.EXPECT().LayerToTime(types.EpochID(1).FirstLayer()).Return(time.Now()).Times(1)
	tpd.Start(context.TODO())

	tpd.onNewEpoch(context.TODO(), types.EpochID(0))
	tpd.onNewEpoch(context.TODO(), types.EpochID(1))
	lid := types.NewLayerID(types.GetLayersPerEpoch()*2 - 1)
	createRandomATXs(t, tpd.store, lid, numATXs)
	tpd.onNewEpoch(context.TODO(), types.EpochID(2))

	got, err := tpd.GetBeacon(types.EpochID(2))
	require.NoError(t, err)
	require.EqualValues(t, got, types.HexToBeacon(types.BootstrapBeacon))

	got, err = tpd.GetBeacon(types.EpochID(3))
	require.Equal(t, errBeaconNotCalculated, err)
	require.Equal(t, types.EmptyBeacon, got)

	tpd.mClock.EXPECT().Unsubscribe(gomock.Any()).Times(1)
	tpd.Close()
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
		createATX(t, tpd.store, lid, tpd.edSigner, 199)
		createRandomATXs(t, tpd.store, lid, numATXs-1)
	}
	finalLayer := types.NewLayerID(types.GetLayersPerEpoch() * uint32(epoch))
	beacon1 := types.RandomBeacon()
	beacon2 := types.RandomBeacon()
	for layer := gLayer.Add(1); layer.Before(finalLayer); layer = layer.Add(1) {
		tpd.mClock.EXPECT().GetCurrentLayer().Return(layer).AnyTimes()
		if layer.FirstInEpoch() {
			tpd.onNewEpoch(context.TODO(), layer.GetEpoch())
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

func TestBeacon_findMostWeightedBeaconForEpoch_NotEnoughBlocks(t *testing.T) {
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

	r := require.New(t)

	theta1, ok := new(big.Float).SetString("0.5")
	r.True(ok)

	theta2, ok := new(big.Float).SetString("0.0")
	r.True(ok)

	tt := []struct {
		name      string
		kappa     uint64
		q         *big.Rat
		w         uint64
		threshold *big.Float
	}{
		{
			name:      "with epoch weight",
			kappa:     40,
			q:         big.NewRat(1, 3),
			w:         60,
			threshold: theta1,
		},
		{
			name:      "zero epoch weight",
			kappa:     40,
			q:         big.NewRat(1, 3),
			w:         0,
			threshold: theta2,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			threshold := atxThresholdFraction(tc.kappa, tc.q, tc.w)
			r.Equal(tc.threshold.String(), threshold.String())
		})
	}
}

func TestBeacon_atxThreshold(t *testing.T) {
	t.Parallel()

	tt := []struct {
		name      string
		kappa     uint64
		q         *big.Rat
		w         uint64
		threshold string
	}{
		{
			name:      "Case 1",
			kappa:     40,
			q:         big.NewRat(1, 3),
			w:         60,
			threshold: "0x8000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 2",
			kappa:     400000,
			q:         big.NewRat(1, 3),
			w:         31744,
			threshold: "0xffffddbb63fcd30f000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 3",
			kappa:     40,
			q:         new(big.Rat).SetFloat64(0.33),
			w:         60,
			threshold: "0x7f8ece00fe541f9f000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			threshold := atxThreshold(tc.kappa, tc.q, tc.w)
			require.Equal(t, tc.threshold, fmt.Sprintf("%#x", threshold))
		})
	}
}

func TestBeacon_proposalPassesEligibilityThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)
	tt := []struct {
		name     string
		kappa    uint64
		q        *big.Rat
		w        uint64
		proposal []byte
		passes   bool
	}{
		{
			name:     "Case 1",
			kappa:    400000,
			q:        big.NewRat(1, 3),
			w:        31744,
			proposal: util.Hex2Bytes("cee191e87d83dc4fbd5e2d40679accf68237b1f09f73f16db5b05ae74b522def9d2ffee56eeb02070277be99a80666ffef9fd4514a51dc37419dd30a791e0f05"),
			passes:   true,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			logger := logtest.New(t).WithName("proposal checker")
			checker := createProposalChecker(logger, tc.kappa, tc.q, tc.w)
			passes := checker.IsProposalEligible(tc.proposal)
			r.EqualValues(tc.passes, passes)
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

	edSgn := signing.NewEdSigner([20]byte{})
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

	signer := signing.NewEdSigner([20]byte{})
	verifier := signing.NewEdVerifier([20]byte{})

	message := []byte{1, 2, 3, 4}

	signature := signer.Sign(message)
	extractedPK, err := verifier.Extract(message, signature)
	r.NoError(err)

	ok := verifier.Verify(extractedPK, message, signature)

	r.Equal(signer.PublicKey().String(), extractedPK.String())
	r.True(ok)
}

func TestBeacon_signAndVerifyVRF(t *testing.T) {
	r := require.New(t)

	signer := signing.NewEdSigner([20]byte{}).VRFSigner()

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
