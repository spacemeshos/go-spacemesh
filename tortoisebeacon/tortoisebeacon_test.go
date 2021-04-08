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
	"github.com/spacemeshos/go-spacemesh/weakcoin"
)

func TestTortoiseBeacon(t *testing.T) {
	t.Parallel()

	requirer := require.New(t)
	conf := TestConfig()

	mwc := weakcoin.Mock{}

	atxPool := activation.NewAtxMemPool()
	logger := log.NewDefault("TortoiseBeacon")
	genesisTime := time.Now().Add(time.Second * 10)
	ld := time.Duration(10) * time.Second

	types.SetLayersPerEpoch(1)

	clock := timesync.NewClock(timesync.RealClock{}, ld, genesisTime, log.NewDefault("clock"))
	clock.StartNotifying()

	sim := service.NewSimulator()
	n1 := sim.NewNode()

	ticker := clock.Subscribe()
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
