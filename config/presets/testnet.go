package presets

import (
	"time"

	apiConfig "github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/common/types/address"
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
		NetworkID: address.TestnetID,
		Accounts: map[string]uint64{
			"0x92e27bcd079ed0470307620f1425284c3b2b1a65749c3894": 100000000000000000,
			"0xa64b188ac44b208b1a4f018262a720e6397b092a9abe242a": 100000000000000000,
			"0x889c1c60e278bc6c4f9196ca935b8d60ade7e74da0993c8c": 100000000000000000,
			"0x590f605eaea1e050108b0fbf8d35acc1eae640951e1925f6": 100000000000000000,
			"0x19adfef28a1e88821a6ad44f95301c781853baf8da572636": 100000000000000000,
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

	addr, err := address.GenerateAddress(conf.Genesis.NetworkID, []byte("1"))
	if err != nil {
		panic("error generate coinbase address for testnet: " + err.Error())
	}
	conf.SMESHING.CoinbaseAccount = addr.String()
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
