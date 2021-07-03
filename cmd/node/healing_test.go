package node

import (
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/eligibility"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestHealing(t *testing.T) {
	r := require.New(t)
	net := service.NewSimulator()
	const (
		numEpochs    = 5 // first 2 epochs are genesis
		numInstances = 5
	)
	cfg := getTestDefaultConfig(numInstances)
	types.SetLayersPerEpoch(int32(cfg.LayersPerEpoch))
	genesisTime := time.Now().Add(20 * time.Second).Format(time.RFC3339)
	rolacle := eligibility.New()
	ld := time.Duration(20) * time.Second
	gTime, err := time.Parse(time.RFC3339, genesisTime)
	if err != nil {
		log.With().Error("cannot parse genesis time", log.Err(err))
	}
	clock := timesync.NewClock(timesync.RealClock{}, ld, gTime, log.NewDefault("clock"))
	poetHarness, err := activation.NewHTTPPoetHarness(false)
	r.NoError(err)
	r.NotNil(poetHarness)
	defer func() {
		r.NoError(poetHarness.Teardown(true))
	}()
	apps := make([]*SpacemeshApp, numInstances)
	for i := 0; i < numInstances; i++ {
		dbStorepath := t.TempDir()
		database.SwitchCreationContext(dbStorepath, t.Name())
		app, err := InitSingleInstance(*cfg, i, genesisTime, dbStorepath, rolacle, poetHarness.HTTPPoetClient, clock, net)
		apps[i] = app
		r.NoError(err)
	}
}
