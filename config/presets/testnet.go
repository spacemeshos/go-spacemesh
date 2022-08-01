package presets

import (
	"time"

	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func init() {
	register("testnet", testnet())
}

func testnet() config.Config {
	conf := config.DefaultConfig()
	conf.Address = types.DefaultTestAddressConfig()

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
			"stest1rxklau52r6ygyxn2638e2vqu0qv98whcmftjvds0r3764": 100000000000000000,
			"stest1jt38hng8nmgywqc8vg83gffgfsajkxn9wjwr39qka8kuj": 100000000000000000,
			"stest15e933zkyfvsgkxj0qxpx9fequcuhkzf2n2lzg2spvwvlf": 100000000000000000,
			"stest13zwpcc8z0z7xcnu3jm9fxkudvzk70e6d5zvnerqjhu96v": 100000000000000000,
			"stest1ty8kqh4w58s9qyytp7lc6ddvc84wvsy4rcvjtas3w5rkm": 100000000000000000,
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

	conf.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte("1")).String()
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
