package tortoisebeacon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

func TestTortoiseBeacon(t *testing.T) {
	requirer := require.New(t)
	conf := TestConfig()

	mwc := MockWeakCoin{}

	atxPool := activation.NewAtxMemPool()
	logger := log.NewDefault("TortoiseBeacon")
	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second
	types.SetLayersPerEpoch(1)
	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()
	ticker := clock.Subscribe()

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	tb := New(conf, n1, atxPool, nil, mwc, ticker, logger)
	requirer.NotNil(tb)

	err := tb.Start()
	requirer.NoError(err)

	epoch := types.EpochID(2)

	t.Logf("Awaiting epoch %v", epoch)
	awaitEpoch(clock, epoch)

	t.Logf("Waiting for beacon value for epoch %v", epoch)

	err = tb.Wait(epoch)
	requirer.NoError(err)

	t.Logf("Beacon value for epoch %v is ready", epoch)

	v, err := tb.Get(epoch)
	requirer.NoError(err)

	expected := "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	requirer.Equal(expected, v.String())

	requirer.NoError(tb.Close())
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

func TestTortoiseBeacon_classifyMessage(t *testing.T) {
	r := require.New(t)

	epoch := types.EpochID(3)
	round := uint64(4)

	tb := TortoiseBeacon{
		currentRounds: map[types.EpochID]uint64{
			epoch: round,
		},
	}

	tt := []struct {
		name    string
		round   uint64
		msgType MessageType
	}{
		{"Timely", 3, TimelyMessage},
		{"Delayed", 2, DelayedMessage},
		{"Late", 1, LateMessage},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m := VotingMessage{RoundID: tc.round}
			result := tb.classifyMessage(m, epoch)
			r.Equal(tc.msgType, result)
		})
	}
}

func Test_hashATXList(t *testing.T) {
	r := require.New(t)

	tt := []struct {
		name     string
		atxList  []types.ATXID
		expected string
	}{
		{
			name:     "Case 1",
			atxList:  []types.ATXID{types.ATXID(types.CalcHash32([]byte("1")))},
			expected: "0x9c2e4d8fe97d881430de4e754b4205b9c27ce96715231cffc4337340cb110280",
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := hashATXList(tc.atxList).String()
			r.Equal(tc.expected, result)
		})
	}
}
