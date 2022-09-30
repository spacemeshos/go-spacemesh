package presets

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func init() {
	register("fastnet", fastnet())
}

func fastnet() config.Config {
	conf := config.DefaultConfig()
	conf.Address = types.DefaultTestAddressConfig()

	conf.API.StartGrpcServices = []string{
		"gateway", "node", "mesh", "globalstate",
		"transaction", "smesher", "debug",
	}
	if err := conf.API.ParseServicesList(); err != nil {
		panic(err)
	}

	conf.BaseConfig.OptFilterThreshold = 90

	conf.HARE.N = 800
	conf.HARE.ExpectedLeaders = 10
	conf.HARE.LimitConcurrent = 5
	conf.HARE.F = 399
	conf.HARE.LimitIterations = 4
	conf.HARE.RoundDuration = 2
	conf.HARE.WakeupDelta = 3

	conf.P2P.TargetOutbound = 10

	conf.Genesis = &config.GenesisConfig{
		ExtraData: "fastnet",
	}

	conf.LayerAvgSize = 50
	conf.SyncRequestTimeout = 1_000
	conf.LayerDurationSec = 15
	conf.LayersPerEpoch = 4

	conf.HareEligibility.ConfidenceParam = 2 // half epoch
	conf.HareEligibility.EpochOffset = 0

	conf.POST.BitsPerLabel = 8
	conf.POST.K1 = 2000
	conf.POST.K2 = 4
	conf.POST.LabelsPerUnit = 32
	conf.POST.MaxNumUnits = 4
	conf.POST.MinNumUnits = 2

	conf.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte("1")).String()
	conf.SMESHING.Start = false
	conf.SMESHING.Opts.ComputeProviderID = 1
	conf.SMESHING.Opts.NumFiles = 1
	conf.SMESHING.Opts.NumUnits = 2
	conf.SMESHING.Opts.Throttle = true

	conf.Beacon.FirstVotingRoundDuration = 10 * time.Second
	conf.Beacon.GracePeriodDuration = 2 * time.Second
	conf.Beacon.ProposalDuration = 2 * time.Second
	conf.Beacon.VotingRoundDuration = 2 * time.Second
	conf.Beacon.WeakCoinRoundDuration = 2 * time.Second
	conf.Beacon.RoundsNumber = 4
	conf.Beacon.BeaconSyncNumBallots = 30
	conf.Beacon.VotesLimit = 100

	return conf
}
