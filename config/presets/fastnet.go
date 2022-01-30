package presets

import (
	"time"

	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/config"
)

func init() {
	register("fastnet", fastnet())
}

func fastnet() config.Config {
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
	conf.HARE.F = 399
	conf.HARE.LimitIterations = 4
	conf.HARE.RoundDuration = 2
	conf.HARE.WakeupDelta = 2

	conf.P2P.TargetOutbound = 10

	conf.Genesis = &apiConfig.GenesisConfig{}

	conf.LayerAvgSize = 50
	conf.SyncRequestTimeout = 1_000
	conf.LayerDurationSec = 15
	conf.LayersPerEpoch = 4

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

	conf.Beacon.FirstVotingRoundDuration = 4 * time.Second
	conf.Beacon.GracePeriodDuration = 2 * time.Second
	conf.Beacon.ProposalDuration = 2 * time.Second
	conf.Beacon.VotingRoundDuration = 2 * time.Second
	conf.Beacon.WeakCoinRoundDuration = 2 * time.Second
	conf.Beacon.RoundsNumber = 4
	conf.Beacon.BeaconSyncNumBallots = 30
	conf.Beacon.VotesLimit = 100

	return conf
}
