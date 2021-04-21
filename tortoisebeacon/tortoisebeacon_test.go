package tortoisebeacon

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/tortoisebeacon/weakcoin"
)

func TestTortoiseBeacon(t *testing.T) {
	t.Parallel()

	requirer := require.New(t)
	conf := TestConfig()

	mwc := weakcoin.RandomMock{}

	logger := log.NewDefault("TortoiseBeacon")
	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second

	types.SetLayersPerEpoch(1)

	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	epoch := types.EpochID(2)
	atxList := []types.ATXID{types.ATXID(types.HexToHash32("0x01"))}
	atxGetter := newMockATXGetter(atxList)

	ticker := clock.Subscribe()
	tb := New(conf, n1, atxGetter, nil, mwc, ticker, logger)
	requirer.NotNil(tb)

	err := tb.Start()
	requirer.NoError(err)

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

func TestTortoiseBeacon_beaconCalcTimeout(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name             string
		roundsNumber     uint64
		roundDurationSec int
		duration         time.Duration
	}{
		{
			name:             "Case 1",
			roundsNumber:     5,
			roundDurationSec: 10,
			duration:         200 * time.Second,
		},
		{
			name:             "Case 2",
			roundsNumber:     300,
			roundDurationSec: 30 * 60,
			duration:         600 * time.Hour,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conf := Config{
				RoundsNumber:  tc.roundsNumber,
				RoundDuration: tc.roundDurationSec,
			}

			tb := New(conf, nil, nil, nil, nil, nil, log.Log{})
			duration := tb.beaconCalcTimeout()
			r.EqualValues(tc.duration, duration)
		})
	}
}

func TestTortoiseBeacon_votingThreshold(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name      string
		theta     float64
		tAve      int
		threshold int
	}{
		{
			name:      "Case 1",
			theta:     0.5,
			tAve:      10,
			threshold: 5,
		},
		{
			name:      "Case 2",
			theta:     0.3,
			tAve:      10,
			threshold: 3,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
				config: Config{
					Theta: tc.theta,
					TAve:  tc.tAve,
				},
			}

			threshold := tb.votingThreshold()
			r.EqualValues(tc.threshold, threshold)
		})
	}
}

func TestTortoiseBeacon_atxThresholdFraction(t *testing.T) {
	t.Parallel()

	r := require.New(t)

	tt := []struct {
		name      string
		kappa     int
		q         float64
		w         int
		threshold float64
	}{
		{
			name:      "Case 1",
			kappa:     40,
			q:         1.0 / 3.0,
			w:         60,
			threshold: 0.5,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tb := TortoiseBeacon{
				Log: log.NewDefault("TortoiseBeacon"),
				config: Config{
					Kappa: tc.kappa,
					Q:     tc.q,
				},
			}

			threshold := tb.atxThresholdFraction(tc.w)
			r.InDelta(tc.threshold, threshold, 0.00001)
		})
	}
}
