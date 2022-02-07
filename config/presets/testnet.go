package presets

import (
	"time"

	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/config"
)

func init() {
	register("testnet", testnet())
}

func testnet() config.Config {
	conf := config.DefaultConfig()

	conf.API.StartGrpcServices = []string{
		"gateway", "node", "mesh", "globalstate",
		"transaction", "smesher", "debug",
	}
	if err := conf.API.ParseServicesList(); err != nil {
		panic(err)
	}

	conf.HARE.N = 800
	conf.HARE.ExpectedLeaders = 10
	conf.HARE.LimitConcurrent = 5
	conf.HARE.LimitIterations = 10
	conf.HARE.F = 399
	conf.HARE.RoundDuration = 10
	conf.HARE.WakeupDelta = 10

	conf.P2P.TargetOutbound = 10

	conf.Genesis = &apiConfig.GenesisConfig{
		Accounts: map[string]uint64{
			"0x8868600d2835019249a0f756f754d5a5ab17ebf0": 100000000000000000,
			"0x9f120e40e330affe6aa27d5bda49ac0960df7d6f": 100000000000000000,
			"0xafed9a1c17ca7eaa7a6795dbc7bee1b1d992c7ba": 100000000000000000,
			"0xb20c3a31f973231e01bf2d6b5c00a22cc13b5c63": 100000000000000000,
			"0xff083c9a22e05d3459b03f3dbed61c7ad6e0a209": 100000000000000000,
		},
	}

	conf.LayerAvgSize = 50
	conf.LayerDurationSec = 120
	conf.LayersPerEpoch = 60
	conf.SyncRequestTimeout = 60_000

	conf.POST.BitsPerLabel = 8
	conf.POST.K1 = 2000
	conf.POST.K2 = 1800
	conf.POST.LabelsPerUnit = 1024
	conf.POST.MaxNumUnits = 4
	conf.POST.MinNumUnits = 2

	conf.SMESHING.CoinbaseAccount = "1"
	conf.SMESHING.Start = false
	conf.SMESHING.Opts.ComputeProviderID = 1
	conf.SMESHING.Opts.NumFiles = 1
	conf.SMESHING.Opts.NumUnits = 2
	conf.SMESHING.Opts.Throttle = true

	conf.Beacon.FirstVotingRoundDuration = 3 * time.Minute
	conf.Beacon.GracePeriodDuration = 10 * time.Second
	conf.Beacon.ProposalDuration = 30 * time.Second
	conf.Beacon.RoundsNumber = 6
	conf.Beacon.BeaconSyncNumBallots = 30
	conf.Beacon.VotesLimit = 100
	conf.Beacon.VotingRoundDuration = 50 * time.Second
	conf.Beacon.WeakCoinRoundDuration = 10 * time.Second

	return conf
}
