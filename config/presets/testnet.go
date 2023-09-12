package presets

import (
	"math"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spacemeshos/post/initialization"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	hareConfig "github.com/spacemeshos/go-spacemesh/hare/config"
	eligConfig "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

func init() {
	register("testnet", testnet())
}

func testnet() config.Config {
	p2pconfig := p2p.DefaultConfig()

	smeshing := config.DefaultSmeshingConfig()
	smeshing.Opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	smeshing.ProvingOpts.Nonces = 288
	smeshing.ProvingOpts.Threads = uint(runtime.NumCPU() * 3 / 4)
	if smeshing.ProvingOpts.Threads < 1 {
		smeshing.ProvingOpts.Threads = 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic("can't read homedir: " + err.Error())
	}
	defaultdir := filepath.Join(home, "spacemesh-testnet", "/")
	return config.Config{
		BaseConfig: config.BaseConfig{
			DataDirParent:       defaultdir,
			FileLock:            filepath.Join(os.TempDir(), "spacemesh.lock"),
			MetricsPort:         1010,
			DatabaseConnections: 16,
			NetworkHRP:          "smtest",

			LayerDuration:  5 * time.Minute,
			LayerAvgSize:   50,
			LayersPerEpoch: 288,

			TxsPerProposal: 700,       // https://github.com/spacemeshos/go-spacemesh/issues/4559
			BlockGasLimit:  100107000, // 3000 of spends

			OptFilterThreshold: 90,

			TickSize:    666514,
			PoETServers: []string{},
		},
		Genesis: &config.GenesisConfig{
			GenesisTime: "2023-09-13T18:00:00Z",
			ExtraData:   "0000000000000000000000c76c58ebac180989673fd6d237b40e66ed5c976ec3",
		},

		Tortoise: tortoise.Config{
			Hdist:                    10,
			Zdist:                    2,
			WindowSize:               10000,
			MaxExceptions:            1000,
			BadBeaconVoteDelayLayers: 4032,
			// 100 - is assumed minimal number of units
			// 100 - half of the expected poet ticks
			MinimalActiveSetWeight: 100 * 100,
		},
		HARE: hareConfig.Config{
			N:               200,
			ExpectedLeaders: 5,
			RoundDuration:   25 * time.Second,
			WakeupDelta:     25 * time.Second,
			LimitConcurrent: 2,
			LimitIterations: 4,
		},
		HareEligibility: eligConfig.Config{
			ConfidenceParam: 20,
		},
		Beacon: beacon.Config{
			Kappa:                    40,
			Q:                        big.NewRat(1, 3),
			Theta:                    big.NewRat(1, 4),
			GracePeriodDuration:      10 * time.Minute,
			ProposalDuration:         4 * time.Minute,
			FirstVotingRoundDuration: 30 * time.Minute,
			RoundsNumber:             20,
			VotingRoundDuration:      4 * time.Minute,
			WeakCoinRoundDuration:    4 * time.Minute,
			VotesLimit:               100,
			BeaconSyncWeightUnits:    800,
		},
		POET: activation.PoetConfig{
			PhaseShift:        12 * time.Hour,
			CycleGap:          2 * time.Hour,
			GracePeriod:       10 * time.Minute,
			RequestRetryDelay: 5 * time.Second,
			MaxRequestRetries: 10,
		},
		POST: activation.PostConfig{
			MinNumUnits:   2,
			MaxNumUnits:   math.MaxUint32,
			LabelsPerUnit: 1024,
			K1:            26,
			K2:            37,
			K3:            37,
			PowDifficulty: activation.DefaultPostConfig().PowDifficulty,
		},
		Bootstrap: bootstrap.Config{
			URL:      "https://bootstrap.spacemesh.network/testnet06",
			Version:  "https://spacemesh.io/bootstrap.schema.json.1.0",
			DataDir:  os.TempDir(),
			Interval: 30 * time.Second,
		},
		P2P:      p2pconfig,
		API:      grpcserver.DefaultConfig(),
		TIME:     timeConfig.DefaultConfig(),
		SMESHING: smeshing,
		FETCH:    fetch.DefaultConfig(),
		LOGGING:  config.DefaultLoggingConfig(),
		Sync: syncer.Config{
			Interval:         time.Minute,
			EpochEndFraction: 0.8,
			MaxStaleDuration: time.Hour,
			UseNewProtocol:   true,
			GossipDuration:   50 * time.Second,
		},
		Recovery: checkpoint.DefaultConfig(),
		Cache:    datastore.DefaultConfig(),
	}
}
