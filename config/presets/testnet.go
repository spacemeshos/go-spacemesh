package presets

import (
	"time"

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
	conf.HARE.RoundDuration = 10 * time.Second
	conf.HARE.WakeupDelta = 10 * time.Second

	conf.P2P.MinPeers = 10

	conf.Genesis = &config.GenesisConfig{
		ExtraData: "testnet",
		Accounts: map[string]uint64{
			"stest1qqqqqqygdpsq62p4qxfyng8h2mm4f4d94vt7huqqu9mz3": 100000000000000000,
			"stest1qqqqqqylzg8ypces4llx4gnat0dyntqfvr0h6mcprcz66": 100000000000000000,
			"stest1qqqqqq90akdpc97206485eu4m0rmacd3mxfv0wsdrea6k": 100000000000000000,
			"stest1qqqqqq9jpsarr7tnyv0qr0edddwqpg3vcya4cccauypts": 100000000000000000,
			"stest1qqqqqq8lpq7f5ghqt569nvpl8kldv8r66ms2yzgudsd5t": 100000000000000000,
		},
	}

	conf.LayerAvgSize = 50
	conf.LayerDuration = 120 * time.Second
	conf.LayersPerEpoch = 60
	conf.SyncRequestTimeout = 60_000

	conf.Tortoise.Hdist = 60
	conf.Tortoise.Zdist = 10
	conf.Tortoise.BadBeaconVoteDelayLayers = 30

	conf.POST.BitsPerLabel = 8
	conf.POST.K1 = 200
	conf.POST.K2 = 212
	conf.POST.LabelsPerUnit = 2048
	conf.POST.MaxNumUnits = 4
	conf.POST.MinNumUnits = 2
	conf.POST.N = 32
	conf.POST.B = 16

	conf.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte("1")).String()
	conf.SMESHING.Start = false
	conf.SMESHING.Opts.ComputeProviderID = 1
	conf.SMESHING.Opts.NumUnits = 2
	conf.SMESHING.Opts.Throttle = true

	conf.Beacon.FirstVotingRoundDuration = 3 * time.Minute
	conf.Beacon.GracePeriodDuration = 10 * time.Second
	conf.Beacon.ProposalDuration = 30 * time.Second
	conf.Beacon.RoundsNumber = 6
	conf.Beacon.BeaconSyncWeightUnits = 30
	conf.Beacon.VotesLimit = 100
	conf.Beacon.VotingRoundDuration = 50 * time.Second
	conf.Beacon.WeakCoinRoundDuration = 10 * time.Second

	return conf
}
