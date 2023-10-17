package presets

import (
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/spacemeshos/post/initialization"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
)

func init() {
	register("standalone", standalone())
}

func standalone() config.Config {
	conf := config.DefaultConfig()
	conf.NetworkHRP = "standalone"
	types.SetNetworkHRP(conf.NetworkHRP) // set to generate coinbase

	conf.TIME.Peersync.Disable = true
	conf.Standalone = true
	conf.DataDirParent = filepath.Join(os.TempDir(), "spacemesh")
	conf.FileLock = filepath.Join(conf.DataDirParent, "LOCK")

	conf.HARE.N = 800
	conf.HARE.ExpectedLeaders = 10
	conf.HARE.LimitConcurrent = 2
	conf.HARE.LimitIterations = 2
	conf.HARE.RoundDuration = 1 * time.Second
	conf.HARE.WakeupDelta = 1 * time.Second

	conf.Genesis = &config.GenesisConfig{
		ExtraData: "standalone",
		Accounts:  map[string]uint64{},
	}

	conf.LayerAvgSize = 50
	conf.LayerDuration = 6 * time.Second
	conf.Sync.Interval = 3 * time.Second
	conf.LayersPerEpoch = 10

	conf.Tortoise.Hdist = 2
	conf.Tortoise.Zdist = 2

	conf.HareEligibility.ConfidenceParam = 2

	conf.POST.K1 = 12
	conf.POST.K2 = 4
	conf.POST.K3 = 4
	conf.POST.LabelsPerUnit = 64
	conf.POST.MaxNumUnits = 2
	conf.POST.MinNumUnits = 1

	conf.SMESHING.CoinbaseAccount = types.GenerateAddress([]byte("1")).String()
	conf.SMESHING.Start = true
	conf.SMESHING.Opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	conf.SMESHING.Opts.NumUnits = 1
	conf.SMESHING.Opts.Throttle = true
	conf.SMESHING.Opts.DataDir = conf.DataDirParent
	conf.SMESHING.ProvingOpts.RandomXMode = activation.PostRandomXModeLight

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

	conf.PoETServers = []string{"http://127.0.0.1:10010"}
	conf.POET.GracePeriod = 12 * time.Second
	conf.POET.CycleGap = 30 * time.Second
	conf.POET.PhaseShift = 30 * time.Second
	conf.POET.RequestTimeout = 12 * time.Second // RequestRetryDelay * 2 * MaxRequestRetries*(MaxRequestRetries+1)/2
	conf.POET.RequestRetryDelay = 1 * time.Second
	conf.POET.MaxRequestRetries = 3

	conf.P2P.DisableNatPort = true

	conf.API.PublicListener = "0.0.0.0:10092"
	conf.API.PrivateListener = "127.0.0.1:10093"

	conf.POSTService.NodeAddress = "http://127.0.0.1:10093"
	return conf
}
