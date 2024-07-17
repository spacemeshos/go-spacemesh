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
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer"
	"github.com/spacemeshos/go-spacemesh/syncer/atxsync"
	"github.com/spacemeshos/go-spacemesh/syncer/malsync"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

func init() {
	register("testnet", testnet())
}

func testnet() config.Config {
	p2pconfig := p2p.DefaultConfig()

	smeshing := config.DefaultSmeshingConfig()
	smeshing.Opts.ProviderID.SetUint32(initialization.CPUProviderID())
	smeshing.ProvingOpts.Nonces = 288
	smeshing.ProvingOpts.Threads = uint(runtime.NumCPU() * 3 / 4)
	if smeshing.ProvingOpts.Threads < 1 {
		smeshing.ProvingOpts.Threads = 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic("can't read homedir: " + err.Error())
	}
	hare3conf := hare3.DefaultConfig()
	hare3conf.Enable = true
	hare3conf.EnableLayer = 7366
	// NOTE(dshulyak) i forgot to set protocol name for testnet when we configured it manually.
	// we can't do rolling upgrade if protocol name changes, so lets keep it like that temporarily.
	hare3conf.ProtocolName = ""
	defaultdir := filepath.Join(home, "spacemesh-testnet", "/")
	return config.Config{
		Preset: "testnet",
		BaseConfig: config.BaseConfig{
			DataDirParent:                defaultdir,
			FileLock:                     filepath.Join(os.TempDir(), "spacemesh.lock"),
			MetricsPort:                  1010,
			DatabaseConnections:          16,
			DatabaseSizeMeteringInterval: 10 * time.Minute,
			DatabasePruneInterval:        30 * time.Minute,
			NetworkHRP:                   "stest",

			LayerDuration:  5 * time.Minute,
			LayerAvgSize:   50,
			LayersPerEpoch: 288,

			TxsPerProposal: 700,       // https://github.com/spacemeshos/go-spacemesh/issues/4559
			BlockGasLimit:  100107000, // 3000 of spends

			OptFilterThreshold: 90,

			TickSize:            666514,
			RegossipAtxInterval: time.Hour,
			ATXGradeDelay:       30 * time.Minute,

			PprofHTTPServerListener: "localhost:6060",
		},
		Genesis: config.GenesisConfig{
			GenesisTime: "2023-09-13T18:00:00Z",
			ExtraData:   "0000000000000000000000c76c58ebac180989673fd6d237b40e66ed5c976ec3",
		},
		Tortoise: tortoise.Config{
			Hdist:                    10,
			Zdist:                    2,
			WindowSize:               10000,
			MaxExceptions:            1000,
			BadBeaconVoteDelayLayers: 4032,
			MinimalActiveSetWeight:   []types.EpochMinimalActiveWeight{{Weight: 10_000}},
		},
		HARE3: hare3conf,
		HareEligibility: eligibility.Config{
			ConfidenceParam: 20,
		},
		Beacon: beacon.Config{
			Kappa:                    40,
			Q:                        *big.NewRat(1, 3),
			Theta:                    *big.NewRat(1, 4),
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
			// RequestTimeout = RequestRetryDelay * 2 * MaxRequestRetries*(MaxRequestRetries+1)/2
			RequestTimeout:    550 * time.Second,
			RequestRetryDelay: 5 * time.Second,
			MaxRequestRetries: 10,
			PhaseShift:        12 * time.Hour,
			CycleGap:          2 * time.Hour,
			GracePeriod:       10 * time.Minute,
		},
		POST: activation.PostConfig{
			MinNumUnits:   2,
			MaxNumUnits:   math.MaxUint32,
			LabelsPerUnit: 1024,
			K1:            26,
			K2:            37,
			K3:            1,
			PowDifficulty: activation.DefaultPostConfig().PowDifficulty,
		},
		POSTService: activation.DefaultPostServiceConfig(),
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
			Interval:                 time.Minute,
			EpochEndFraction:         0.5,
			MaxStaleDuration:         time.Hour,
			GossipDuration:           50 * time.Second,
			OutOfSyncThresholdLayers: 10,
			AtxSync:                  atxsync.DefaultConfig(),
			MalSync:                  malsync.DefaultConfig(),
		},
		Recovery: checkpoint.DefaultConfig(),
		Cache:    datastore.DefaultConfig(),
		Certificate: blocks.CertConfig{
			// NOTE(dshulyak) this is intentional. we increased committee size with hare3 upgrade
			// but certifier continues to use 200 committee size.
			// this will be upgraded in future with scheduled upgrade.
			CommitteeSize: 200,
		},
		ActiveSet: miner.ActiveSetPreparation{
			Window:        10 * time.Minute,
			RetryInterval: time.Minute,
			Tries:         5,
		},
		Certifier: activation.DefaultCertifierConfig(),
	}
}
