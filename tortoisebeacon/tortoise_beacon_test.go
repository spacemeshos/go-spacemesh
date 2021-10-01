package tortoisebeacon

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	dbMocks "github.com/spacemeshos/go-spacemesh/database/mocks"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/p2p/p2pcrypto"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	p2pMocks "github.com/spacemeshos/go-spacemesh/p2p/service/mocks"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/mocks"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSyncState bool

func (ss testSyncState) IsSynced(context.Context) bool {
	return bool(ss)
}

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

func setUpTortoiseBeacon(t *testing.T, mockEpochWeight uint64, hasATX bool) (*TortoiseBeacon, *timesync.TimeClock) {
	conf := UnitTestConfig()

	ctrl := gomock.NewController(t)
	mockDB := mocks.NewMockactivationDB(ctrl)
	if hasATX {
		mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).Return(types.ATXID{}, nil).MaxTimes(3)
	} else {
		mockDB.EXPECT().GetNodeAtxIDForEpoch(gomock.Any(), gomock.Any()).Return(types.ATXID{}, database.ErrNotFound).MaxTimes(3)
	}
	// epoch 2, 3, and 4 (since we waited til epoch 3 in each test)
	mockDB.EXPECT().GetEpochWeight(gomock.Any()).Return(mockEpochWeight, nil, nil).MaxTimes(3)
	mwc := coinValueMock(t, true)

	types.SetLayersPerEpoch(3)
	logger := logtest.New(t).WithName("TortoiseBeacon")
	genesisTime := time.Now().Add(100 * time.Millisecond)
	ld := 100 * time.Millisecond
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, logtest.New(t).WithName("clock"))
	clock.StartNotifying()

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()
	vrfSigner, vrfPub, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	require.NoError(t, err)

	node := service.NewSimulator().NewNode()
	minerID := types.NodeID{Key: edPubkey.String(), VRFPublicKey: vrfPub}
	tb := New(conf, minerID, node, mockDB, edSgn, signing.NewEDVerifier(), vrfSigner, signing.VRFVerifier{}, mwc, database.NewMemDatabase(), clock, logger)
	require.NotNil(t, tb)
	tb.SetSyncState(testSyncState(true))
	return tb, clock
}

func TestTortoiseBeacon(t *testing.T) {
	t.Parallel()

	tb, clock := setUpTortoiseBeacon(t, uint64(10), true)
	epoch := types.EpochID(3)
	err := tb.Start(context.TODO())
	require.NoError(t, err)

	t.Logf("Awaiting epoch %v", epoch)
	awaitEpoch(clock, epoch)

	v, err := tb.GetBeacon(epoch)
	require.NoError(t, err)

	expected := "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	assert.Equal(t, expected, types.BytesToHash(v).String())

	tb.Close()
	clock.Close()
}

func TestTortoiseBeaconZeroWeightEpoch(t *testing.T) {
	t.Parallel()

	tb, clock := setUpTortoiseBeacon(t, uint64(0), true)
	epoch := types.EpochID(3)
	err := tb.Start(context.TODO())
	require.NoError(t, err)

	t.Logf("Awaiting epoch %v", epoch)
	awaitEpoch(clock, epoch)

	v, err := tb.GetBeacon(epoch)
	assert.Equal(t, ErrBeaconNotCalculated, err)
	assert.Nil(t, v)

	tb.Close()
	clock.Close()
}

func TestTortoiseBeaconNoATXInPreviousEpoch(t *testing.T) {
	t.Parallel()

	tb, clock := setUpTortoiseBeacon(t, uint64(0), false)
	epoch := types.EpochID(3)
	err := tb.Start(context.TODO())
	require.NoError(t, err)

	t.Logf("Awaiting epoch %v", epoch)
	awaitEpoch(clock, epoch)

	v, err := tb.GetBeacon(epoch)
	assert.Equal(t, ErrBeaconNotCalculated, err)
	assert.Nil(t, v)

	tb.Close()
	clock.Close()
}

func awaitEpoch(clock *timesync.TimeClock, epoch types.EpochID) {
	layerTicker := clock.Subscribe()

	for layer := range layerTicker {
		// Wait until required epoch passes.
		if layer.GetEpoch() > epoch {
			return
		}
	}
}

func TestTortoiseBeacon_BeaconsWithDatabase(t *testing.T) {
	t.Parallel()

	tb := &TortoiseBeacon{
		logger:  logtest.New(t).WithName("TortoiseBeacon"),
		beacons: make(map[types.EpochID]types.Hash32),
		db:      database.NewMemDatabase(),
	}
	epoch3 := types.EpochID(3)
	beacon2 := types.HexToHash32("0x12345678")
	epoch5 := types.EpochID(5)
	beacon4 := types.HexToHash32("0x23456789")
	err := tb.setBeacon(epoch3-1, beacon2)
	require.NoError(t, err)
	err = tb.setBeacon(epoch5-1, beacon4)
	require.NoError(t, err)

	got, err := tb.GetBeacon(epoch3)
	assert.NoError(t, err)
	assert.Equal(t, beacon2.Bytes(), got)

	got, err = tb.GetBeacon(epoch5)
	assert.NoError(t, err)
	assert.Equal(t, beacon4.Bytes(), got)

	got, err = tb.GetBeacon(epoch5 - 1)
	assert.Equal(t, ErrBeaconNotCalculated, err)
	assert.Empty(t, got)

	// clear out the in-memory map
	// the database should still give us values
	tb.mu.Lock()
	tb.beacons = make(map[types.EpochID]types.Hash32)
	tb.mu.Unlock()

	got, err = tb.GetBeacon(epoch3)
	assert.NoError(t, err)
	assert.Equal(t, beacon2.Bytes(), got)

	got, err = tb.GetBeacon(epoch5)
	assert.NoError(t, err)
	assert.Equal(t, beacon4.Bytes(), got)

	got, err = tb.GetBeacon(epoch5 - 1)
	assert.Equal(t, ErrBeaconNotCalculated, err)
	assert.Empty(t, got)
}

func TestTortoiseBeacon_BeaconsWithDatabaseFailure(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockDB := dbMocks.NewMockDatabase(ctrl)
	tb := &TortoiseBeacon{
		logger:  logtest.New(t).WithName("TortoiseBeacon"),
		beacons: make(map[types.EpochID]types.Hash32),
		db:      mockDB,
	}
	epoch := types.EpochID(3)
	beacon := types.HexToHash32("0x12345678")
	mockDB.EXPECT().Put(epoch.ToBytes(), beacon.Bytes()).Return(errUnknown).Times(1)
	err := tb.persistBeacon(epoch, beacon)
	assert.Equal(t, err, errUnknown)

	mockDB.EXPECT().Get(epoch.ToBytes()).Return(nil, errUnknown).Times(1)
	got, errGet := tb.getPersistedBeacon(epoch)
	assert.Nil(t, got)
	assert.Equal(t, errGet, errUnknown)

	ctrl.Finish()
}

func TestTortoiseBeacon_BeaconsCleanupOldEpoch(t *testing.T) {
	t.Parallel()

	tb := &TortoiseBeacon{
		logger:            logtest.New(t).WithName("TortoiseBeacon"),
		db:                database.NewMemDatabase(),
		beacons:           make(map[types.EpochID]types.Hash32),
		beaconsFromBlocks: make(map[types.EpochID]map[string]*epochBeacon),
	}

	epoch := types.EpochID(5)
	for i := 0; i < numEpochsToKeep; i++ {
		e := epoch + types.EpochID(i)
		err := tb.setBeacon(e, randomHash())
		require.NoError(t, err)
		tb.recordBlockBeacon(e, randomBlockID(), randomHash().Bytes(), 10)
		tb.cleanupEpoch(e)
		assert.Equal(t, i+1, len(tb.beacons))
		assert.Equal(t, i+1, len(tb.beaconsFromBlocks))
	}
	assert.Equal(t, numEpochsToKeep, len(tb.beacons))
	assert.Equal(t, numEpochsToKeep, len(tb.beaconsFromBlocks))

	epoch = epoch + numEpochsToKeep
	err := tb.setBeacon(epoch, randomHash())
	require.NoError(t, err)
	tb.recordBlockBeacon(epoch, randomBlockID(), randomHash().Bytes(), 10)
	assert.Equal(t, numEpochsToKeep+1, len(tb.beacons))
	assert.Equal(t, numEpochsToKeep+1, len(tb.beaconsFromBlocks))
	tb.cleanupEpoch(epoch)
	assert.Equal(t, numEpochsToKeep, len(tb.beacons))
	assert.Equal(t, numEpochsToKeep, len(tb.beaconsFromBlocks))
}

func randomHash() types.Hash32 {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that Err == nil only if we read len(b) bytes.
	if err != nil {
		return types.Hash32{}
	}
	return types.CalcHash32(b)
}

// RandomBlockID generates random block id
func randomBlockID() types.BlockID {
	b := make([]byte, 8)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return types.BlockID{}
	}
	return types.BlockID(types.CalcHash32(b).ToHash20())
}

func TestTortoiseBeacon_ReportBeaconFromBlock(t *testing.T) {
	t.Parallel()

	types.SetLayersPerEpoch(3)
	tb := &TortoiseBeacon{
		logger:            logtest.New(t).WithName("TortoiseBeacon"),
		config:            UnitTestConfig(),
		db:                database.NewMemDatabase(),
		beacons:           make(map[types.EpochID]types.Hash32),
		beaconsFromBlocks: make(map[types.EpochID]map[string]*epochBeacon),
	}
	tb.config.BeaconSyncNumBlocks = 3

	epoch := types.EpochID(3)
	beacon1 := randomHash().Bytes()
	beacon2 := randomHash().Bytes()
	tb.ReportBeaconFromBlock(epoch, randomBlockID(), beacon1, 100)
	got, err := tb.GetBeacon(epoch)
	require.Equal(t, ErrBeaconNotCalculated, err)
	require.Nil(t, got)
	tb.ReportBeaconFromBlock(epoch, randomBlockID(), beacon2, 100)
	got, err = tb.GetBeacon(epoch)
	require.Equal(t, ErrBeaconNotCalculated, err)
	require.Nil(t, got)
	tb.ReportBeaconFromBlock(epoch, randomBlockID(), beacon1, 1)
	got, err = tb.GetBeacon(epoch)
	assert.NoError(t, err)
	assert.Equal(t, beacon1, got)
}

func TestTortoiseBeacon_ReportBeaconFromBlock_SameBlock(t *testing.T) {
	t.Parallel()

	types.SetLayersPerEpoch(3)
	tb := &TortoiseBeacon{
		logger:            logtest.New(t).WithName("TortoiseBeacon"),
		config:            UnitTestConfig(),
		db:                database.NewMemDatabase(),
		beacons:           make(map[types.EpochID]types.Hash32),
		beaconsFromBlocks: make(map[types.EpochID]map[string]*epochBeacon),
	}
	tb.config.BeaconSyncNumBlocks = 2

	epoch := types.EpochID(3)
	beacon1 := randomHash().Bytes()
	beacon2 := randomHash().Bytes()
	blockID1 := randomBlockID()
	blockID2 := randomBlockID()
	tb.ReportBeaconFromBlock(epoch, blockID1, beacon1, 100)
	tb.ReportBeaconFromBlock(epoch, blockID1, beacon1, 200)
	// same blockID does not count twice
	got, err := tb.GetBeacon(epoch)
	require.Equal(t, ErrBeaconNotCalculated, err)
	require.Nil(t, got)

	tb.ReportBeaconFromBlock(epoch, blockID2, beacon2, 101)
	got, err = tb.GetBeacon(epoch)
	assert.NoError(t, err)
	assert.Equal(t, beacon2, got)
}

func TestTortoiseBeacon_ensureEpochHasBeacon_BeaconAlreadyCalculated(t *testing.T) {
	t.Parallel()

	epoch := types.EpochID(3)
	beacon := randomHash()
	beaconFromBlocks := randomHash().Bytes()
	tb := &TortoiseBeacon{
		logger: logtest.New(t).WithName("TortoiseBeacon"),
		config: UnitTestConfig(),
		beacons: map[types.EpochID]types.Hash32{
			epoch - 1: beacon,
		},
		beaconsFromBlocks: make(map[types.EpochID]map[string]*epochBeacon),
	}
	tb.config.BeaconSyncNumBlocks = 2

	got, err := tb.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon.Bytes(), got)

	tb.ReportBeaconFromBlock(epoch, randomBlockID(), beaconFromBlocks, 100)
	tb.ReportBeaconFromBlock(epoch, randomBlockID(), beaconFromBlocks, 200)

	// should not change the beacon value
	got, err = tb.GetBeacon(epoch)
	require.NoError(t, err)
	require.Equal(t, beacon.Bytes(), got)
}

func TestTortoiseBeacon_findMostWeightedBeaconForEpoch(t *testing.T) {
	t.Parallel()

	types.SetLayersPerEpoch(3)
	beacon1 := randomHash().Bytes()
	beacon2 := randomHash().Bytes()
	beacon3 := randomHash().Bytes()

	beaconsFromBlocks := map[string]*epochBeacon{
		string(beacon1): {
			blocks: map[types.BlockID]struct{}{randomBlockID(): {}, randomBlockID(): {}},
			weight: 200,
		},
		string(beacon2): {
			blocks: map[types.BlockID]struct{}{randomBlockID(): {}},
			weight: 201,
		},
		string(beacon3): {
			blocks: map[types.BlockID]struct{}{randomBlockID(): {}},
			weight: 200,
		},
	}
	epoch := types.EpochID(3)
	tb := &TortoiseBeacon{
		logger:            logtest.New(t).WithName("TortoiseBeacon"),
		config:            UnitTestConfig(),
		beacons:           make(map[types.EpochID]types.Hash32),
		beaconsFromBlocks: map[types.EpochID]map[string]*epochBeacon{epoch: beaconsFromBlocks},
	}
	tb.config.BeaconSyncNumBlocks = 2
	got := tb.findMostWeightedBeaconForEpoch(epoch)
	assert.Equal(t, beacon2, got)
}

func TestTortoiseBeacon_findMostWeightedBeaconForEpoch_NotEnoughBlocks(t *testing.T) {
	t.Parallel()

	types.SetLayersPerEpoch(3)
	beacon1 := randomHash().Bytes()
	beacon2 := randomHash().Bytes()
	beacon3 := randomHash().Bytes()

	beaconsFromBlocks := map[string]*epochBeacon{
		string(beacon1): {
			blocks: map[types.BlockID]struct{}{randomBlockID(): {}, randomBlockID(): {}},
			weight: 200,
		},
		string(beacon2): {
			blocks: map[types.BlockID]struct{}{randomBlockID(): {}},
			weight: 201,
		},
		string(beacon3): {
			blocks: map[types.BlockID]struct{}{randomBlockID(): {}},
			weight: 200,
		},
	}
	epoch := types.EpochID(3)
	tb := &TortoiseBeacon{
		logger:            logtest.New(t).WithName("TortoiseBeacon"),
		config:            UnitTestConfig(),
		beacons:           make(map[types.EpochID]types.Hash32),
		beaconsFromBlocks: map[types.EpochID]map[string]*epochBeacon{epoch: beaconsFromBlocks},
	}
	tb.config.BeaconSyncNumBlocks = 5
	got := tb.findMostWeightedBeaconForEpoch(epoch)
	assert.Nil(t, got)
}

func TestTortoiseBeacon_findMostWeightedBeaconForEpoch_NoBeacon(t *testing.T) {
	t.Parallel()

	types.SetLayersPerEpoch(3)
	tb := &TortoiseBeacon{
		logger:            logtest.New(t).WithName("TortoiseBeacon"),
		config:            UnitTestConfig(),
		beacons:           make(map[types.EpochID]types.Hash32),
		beaconsFromBlocks: make(map[types.EpochID]map[string]*epochBeacon),
	}
	epoch := types.EpochID(3)
	got := tb.findMostWeightedBeaconForEpoch(epoch)
	assert.Nil(t, got)
}

func TestTortoiseBeacon_getProposalChannel(t *testing.T) {
	t.Parallel()

	currentEpoch := types.EpochID(10)

	tt := []struct {
		name            string
		epoch           types.EpochID
		proposalEndTime time.Time
		makeNextChFull  bool
		expected        bool
	}{
		{
			name:     "old epoch",
			epoch:    currentEpoch - 1,
			expected: false,
		},
		{
			name:     "on time",
			epoch:    currentEpoch,
			expected: true,
		},
		{
			name:            "proposal phase ended",
			epoch:           currentEpoch,
			proposalEndTime: time.Now(),
			expected:        false,
		},
		{
			name:     "accept next epoch",
			epoch:    currentEpoch + 1,
			expected: true,
		},
		{
			name:           "next epoch full",
			epoch:          currentEpoch + 1,
			makeNextChFull: true,
			expected:       true,
		},
		{
			name:     "no future epoch beyond next",
			epoch:    currentEpoch + 2,
			expected: false,
		},
	}

	for i, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// don't run these tests in parallel. somehow all cases end up using the same TortoiseBeacon instance
			// t.Parallel()
			tb := TortoiseBeacon{
				logger:                    logtest.New(t).WithName(fmt.Sprintf("TortoiseBeacon-%v", i)),
				proposalChans:             make(map[types.EpochID]chan *proposalMessageWithReceiptData),
				epochInProgress:           currentEpoch,
				proposalPhaseFinishedTime: tc.proposalEndTime,
			}

			if tc.makeNextChFull {
				ctrl := gomock.NewController(t)
				tb.mu.Lock()
				nextCh := tb.getOrCreateProposalChannel(currentEpoch + 1)
				for i := 0; i < proposalChanCapacity; i++ {
					mockGossip := p2pMocks.NewMockGossipMessage(ctrl)
					if i == 0 {
						mockGossip.EXPECT().Sender().Return(p2pcrypto.NewRandomPubkey()).Times(1)
					}
					nextCh <- &proposalMessageWithReceiptData{
						gossip:  mockGossip,
						message: ProposalMessage{},
					}
				}
				tb.mu.Unlock()
			}

			ch := tb.getProposalChannel(context.TODO(), tc.epoch)
			if tc.expected {
				assert.NotNil(t, ch)
			} else {
				assert.Nil(t, ch)
			}
		})
	}
}

func TestTortoiseBeacon_votingThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name      string
		theta     *big.Rat
		weight    uint64
		threshold *big.Int
	}{
		{
			name:      "Case 1",
			theta:     big.NewRat(1, 2),
			weight:    10,
			threshold: big.NewInt(5),
		},
		{
			name:      "Case 2",
			theta:     big.NewRat(3, 10),
			weight:    10,
			threshold: big.NewInt(3),
		},
		{
			name:      "Case 3",
			theta:     big.NewRat(1, 25000),
			weight:    31744,
			threshold: big.NewInt(1),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				logger: logtest.New(t).WithName("TortoiseBeacon"),
				config: Config{
					Theta: tc.theta,
				},
			}

			threshold := tb.votingThreshold(tc.weight)
			r.EqualValues(tc.threshold, threshold)
		})
	}
}

func TestTortoiseBeacon_atxThresholdFraction(t *testing.T) {
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

func TestTortoiseBeacon_atxThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)
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
			threshold: "0x80000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 2",
			kappa:     400000,
			q:         big.NewRat(1, 3),
			w:         31744,
			threshold: "0xffffddbb63fcd30f0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			name:      "Case 3",
			kappa:     40,
			q:         new(big.Rat).SetFloat64(0.33),
			w:         60,
			threshold: "0x7f8ece00fe541f9f0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			threshold := atxThreshold(tc.kappa, tc.q, tc.w)
			r.EqualValues(tc.threshold, fmt.Sprintf("%#x", threshold))
		})
	}
}

func TestTortoiseBeacon_proposalPassesEligibilityThreshold(t *testing.T) {
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

			checker := createProposalChecker(tc.kappa, tc.q, tc.w, logtest.New(t).WithName("proposal checker"))
			passes := checker.IsProposalEligible(tc.proposal)
			r.EqualValues(tc.passes, passes)
		})
	}
}

func TestTortoiseBeacon_buildProposal(t *testing.T) {
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
			result: string(util.Hex2Bytes("000000035442500012345678")),
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

func TestTortoiseBeacon_signMessage(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	edSgn := signing.NewEdSigner()

	tt := []struct {
		name    string
		message interface{}
		result  []byte
	}{
		{
			name:    "Case 1",
			message: []byte{},
			result:  edSgn.Sign([]byte{0, 0, 0, 0}),
		},
		{
			name:    "Case 2",
			message: &struct{ Test int }{Test: 0x12345678},
			result:  edSgn.Sign([]byte{0x12, 0x34, 0x56, 0x78}),
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := signMessage(edSgn, tc.message, logtest.New(t))
			r.Equal(string(tc.result), string(result))
		})
	}
}

func TestTortoiseBeacon_getSignedProposal(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	edSgn := signing.NewEdSigner()
	edPubkey := edSgn.PublicKey()

	vrfSigner, _, err := signing.NewVRFSigner(edSgn.Sign(edPubkey.Bytes()))
	r.NoError(err)

	tt := []struct {
		name   string
		epoch  types.EpochID
		result []byte
	}{
		{
			name:   "Case 1",
			epoch:  1,
			result: vrfSigner.Sign(util.Hex2Bytes("000000035442500000000001")),
		},
		{
			name:   "Case 2",
			epoch:  2,
			result: vrfSigner.Sign(util.Hex2Bytes("000000035442500000000002")),
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

func TestTortoiseBeacon_signAndExtractED(t *testing.T) {
	r := require.New(t)

	signer := signing.NewEdSigner()
	verifier := signing.NewEDVerifier()

	message := []byte{1, 2, 3, 4}

	signature := signer.Sign(message)
	extractedPK, err := verifier.Extract(message, signature)
	r.NoError(err)

	ok := verifier.Verify(extractedPK, message, signature)

	r.Equal(signer.PublicKey().String(), extractedPK.String())
	r.True(ok)
}

func TestTortoiseBeacon_signAndVerifyVRF(t *testing.T) {
	r := require.New(t)

	signer, _, err := signing.NewVRFSigner(bytes.Repeat([]byte{0x01}, 32))
	r.NoError(err)

	verifier := signing.VRFVerifier{}

	message := []byte{1, 2, 3, 4}

	signature := signer.Sign(message)
	ok := verifier.Verify(signer.PublicKey(), message, signature)
	r.True(ok)
}
