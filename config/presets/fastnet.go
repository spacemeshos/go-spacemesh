package presets

import (
	"math"
	"math/big"
	"time"

	postCfg "github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func init() {
	register("fastnet", fastnet())
}

func fastnet() config.Config {
	conf := config.DefaultConfig()

	conf.NetworkHRP = "stest"
	types.SetNetworkHRP(conf.NetworkHRP) // set to generate coinbase
	conf.BaseConfig.OptFilterThreshold = 90
	conf.BaseConfig.DatabasePruneInterval = time.Minute

	// set for systest TestEquivocation
	conf.BaseConfig.MinerGoodAtxsPercent = 50

	conf.HARE.Disable = 1 // non-zero low layer will prevent hare1 from running
	conf.HARE.N = 800
	conf.HARE.ExpectedLeaders = 10
	conf.HARE.LimitConcurrent = 5
	conf.HARE.LimitIterations = 3
	conf.HARE.RoundDuration = 2 * time.Second
	conf.HARE.WakeupDelta = 3 * time.Second

	// node will select atxs that were received atleast 4 seconds before start of the epoch
	// for activeset.
	// if some atxs weren't received on time it will skew eligibility distribution
	// and will make some tests fail.
	conf.ATXGradeDelay = 1 * time.Second

	conf.HARE3.Enable = true
	conf.HARE3.DisableLayer = types.LayerID(math.MaxUint32)
	conf.HARE3.Committee = 800
	conf.HARE3.Leaders = 10
	conf.HARE3.PreroundDelay = 3 * time.Second
	conf.HARE3.RoundDuration = 700 * time.Millisecond
	conf.HARE3.IterationsLimit = 2

	conf.P2P.MinPeers = 10

	conf.Genesis = &config.GenesisConfig{
		ExtraData: "fastnet",
		Accounts:  map[string]uint64{},
	}

	conf.LayerAvgSize = 50
	conf.LayerDuration = 15 * time.Second
	conf.Sync.Interval = 5 * time.Second
	conf.Sync.GossipDuration = 10 * time.Second
	conf.LayersPerEpoch = 4
	conf.RegossipAtxInterval = 30 * time.Second

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
	conf.SMESHING.Opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	conf.SMESHING.Opts.NumUnits = 2
	conf.SMESHING.Opts.ComputeBatchSize = 128
	conf.SMESHING.Opts.Scrypt.N = 2 // faster scrypt
	// Override proof of work flags to use light mode (less memory intensive)
	conf.SMESHING.ProvingOpts.Flags = postCfg.RecommendedPowFlags()

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

	conf.POET.GracePeriod = 10 * time.Second
	conf.POET.CycleGap = 30 * time.Second
	conf.POET.PhaseShift = 30 * time.Second
	conf.POET.RequestTimeout = 12 * time.Second // RequestRetryDelay * 2 * MaxRequestRetries*(MaxRequestRetries+1)/2
	conf.POET.RequestRetryDelay = 1 * time.Second
	conf.POET.MaxRequestRetries = 3

	return conf
}
