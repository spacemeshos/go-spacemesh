package presets

import (
	"math/big"
	"time"

	"github.com/spacemeshos/post/initialization"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func init() {
	register("fastnet", fastnet())
}

func fastnet() config.Config {
	conf := config.DefaultConfig()
	conf.Address = types.DefaultTestAddressConfig()

	conf.BaseConfig.OptFilterThreshold = 90

	conf.HARE.N = 800
	conf.HARE.ExpectedLeaders = 10
	conf.HARE.LimitConcurrent = 5
	conf.HARE.LimitIterations = 3
	conf.HARE.RoundDuration = 2 * time.Second
	conf.HARE.WakeupDelta = 3 * time.Second

	conf.P2P.MinPeers = 10

	conf.Genesis = &config.GenesisConfig{
		ExtraData: "fastnet",
	}

	conf.LayerAvgSize = 50
	conf.LayerDuration = 15 * time.Second
	conf.Sync.Interval = 5 * time.Second
	conf.LayersPerEpoch = 4

	conf.Tortoise.Hdist = 4
	conf.Tortoise.Zdist = 2
	conf.Tortoise.BadBeaconVoteDelayLayers = 2

	conf.HareEligibility.ConfidenceParam = 2

	conf.POST.K1 = 12
	conf.POST.K2 = 4
	conf.POST.K3 = 4
	conf.POST.LabelsPerUnit = 128
	conf.POST.MaxNumUnits = 4
	conf.POST.MinNumUnits = 2

	conf.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte("1")).String()
	conf.SMESHING.Start = false
	conf.SMESHING.Opts.ProviderID = int(initialization.CPUProviderID())
	conf.SMESHING.Opts.NumUnits = 2
	conf.SMESHING.Opts.Throttle = true

	conf.Beacon.Kappa = 40
	conf.Beacon.Theta = big.NewRat(1, 4)
	conf.Beacon.FirstVotingRoundDuration = 10 * time.Second
	conf.Beacon.GracePeriodDuration = 30 * time.Second
	conf.Beacon.ProposalDuration = 2 * time.Second
	conf.Beacon.VotingRoundDuration = 2 * time.Second
	conf.Beacon.WeakCoinRoundDuration = 2 * time.Second
	conf.Beacon.RoundsNumber = 4
	conf.Beacon.BeaconSyncWeightUnits = 10
	conf.Beacon.VotesLimit = 100

	conf.Recovery.RecoverFromDefaultDir = true
	return conf
}
