package config

import (
	"math"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/fetch"
	hareConfig "github.com/spacemeshos/go-spacemesh/hare/config"
	eligConfig "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

func MainnetConfig() Config {
	var postPowDifficulty activation.PowDifficulty
	if err := postPowDifficulty.UnmarshalText([]byte("00037ec8ec25e6d2c00000000000000000000000000000000000000000000000")); err != nil {
		panic(err)
	}
	p2pconfig := p2p.DefaultConfig()
	p2pconfig.Bootnodes = []string{
		"/dns4/mainnet-bootnode-0.spacemesh.network/tcp/5000/p2p/12D3KooWPStnitMbLyWAGr32gHmPr538mT658Thp6zTUujZt3LRf",
		"/dns4/mainnet-bootnode-1.spacemesh.network/tcp/5000/p2p/12D3KooWJubSzXMJ6f1Fd1PgAFvuVeGFAsGe6tamH1733x4524gb",
		"/dns4/mainnet-bootnode-2.spacemesh.network/tcp/5000/p2p/12D3KooWAsMgXLpyGdsRNjHBF3FaXwnXhyMEqWQYBXUpvCHNzFNK",
		"/dns4/mainnet-bootnode-3.spacemesh.network/tcp/5000/p2p/12D3KooWLu9jbmTemp8eCzNvAFT88u7urfywYqQjXiBQAUCiFAFq",
		"/dns4/mainnet-bootnode-4.spacemesh.network/tcp/5000/p2p/12D3KooWRcTWDHzptnhJn5h6CtwnokzzMaDLcXv6oM9CxQEXd5FL",
		"/dns4/mainnet-bootnode-5.spacemesh.network/tcp/5000/p2p/12D3KooWP8fFtK7pfgdLRAZCwYsxC2pCxXDhnTiadtiCQdiyCouw",
		"/dns4/mainnet-bootnode-6.spacemesh.network/tcp/5000/p2p/12D3KooWRS47KAs3ZLkBtE2AqjJCwxRYqZKmyLkvombJJdrca8Hz",
		"/dns4/mainnet-bootnode-7.spacemesh.network/tcp/5000/p2p/12D3KooWNDCmQUoFPCUWbdYC2owaAXkVYQz7B37myTieiH7xzF6F",
		"/dns4/mainnet-bootnode-8.spacemesh.network/tcp/5000/p2p/12D3KooWFYv99aGbtXnZQy6UZxyf72NpkWJp3K4HS8Py35WhKtzE",
		"/dns4/mainnet-bootnode-9.spacemesh.network/tcp/5000/p2p/12D3KooWRQ9VPotEBjmAxBeTnnLqQgXLi1vvt4nMwo5E3Smt5bXP",
	}
	return Config{
		BaseConfig: BaseConfig{
			DataDirParent:       defaultDataDir,
			FileLock:            filepath.Join(os.TempDir(), "spacemesh.lock"),
			MetricsPort:         1010,
			MetricsPushPeriod:   60,
			DatabaseConnections: 16,
			NetworkHRP:          "sm",

			LayerDuration:  5 * time.Minute,
			LayersPerEpoch: 4032,

			TxsPerProposal: 700,       // https://github.com/spacemeshos/go-spacemesh/issues/4559
			BlockGasLimit:  100107000, // 3000 of spends

			OptFilterThreshold: 90,

			TickSize: 9331200,
			PoETServers: []string{
				"https://mainnet-poet-0.spacemesh.network",
				"https://mainnet-poet-1.spacemesh.network",
				"https://mainnet-poet-2.spacemesh.network",
				"https://poet-110.spacemesh.network",
				"https://poet-111.spacemesh.network",
			},
		},
		Genesis: &GenesisConfig{
			GenesisTime: "2023-07-14T08:00:00Z",
			ExtraData:   "00000000000000000001a6bc150307b5c1998045752b3c87eccf3c013036f3cc",
			Accounts:    MainnetAccounts(),
		},
		Tortoise: tortoise.Config{
			Hdist:                    200,
			Zdist:                    2,
			WindowSize:               10000,
			MaxExceptions:            1000,
			BadBeaconVoteDelayLayers: 4032,
			// TODO update it with safe but reasonble minimum weight in network before first ballot
			MinimalActiveSetWeight: 1000 * 9331200,
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
			ConfidenceParam: 200,
		},
		Beacon: beacon.Config{
			Kappa:                    40,
			Q:                        big.NewRat(1, 3),
			Theta:                    big.NewRat(1, 4),
			GracePeriodDuration:      10 * time.Minute,
			ProposalDuration:         4 * time.Minute,
			FirstVotingRoundDuration: 30 * time.Minute,
			RoundsNumber:             300,
			VotingRoundDuration:      4 * time.Minute,
			WeakCoinRoundDuration:    4 * time.Minute,
			VotesLimit:               100,
			BeaconSyncWeightUnits:    800,
		},
		POET: activation.PoetConfig{
			PhaseShift:        240 * time.Hour,
			CycleGap:          12 * time.Hour,
			GracePeriod:       1 * time.Hour,
			RequestRetryDelay: 10 * time.Second,
			MaxRequestRetries: 10,
		},
		POST: activation.PostConfig{
			MinNumUnits:   4,
			MaxNumUnits:   math.MaxUint32,
			LabelsPerUnit: 4294967296,
			K1:            26,
			K2:            37,
			K3:            37,
			PowDifficulty: postPowDifficulty,
		},
		Bootstrap: bootstrap.Config{
			URL:      "https://bootstrap.spacemesh.network/mainnet",
			Version:  "https://spacemesh.io/bootstrap.schema.json.1.0",
			DataDir:  os.TempDir(),
			Interval: 30 * time.Second,
		},
		P2P:      p2pconfig,
		API:      grpcserver.DefaultConfig(),
		TIME:     timeConfig.DefaultConfig(),
		SMESHING: DefaultSmeshingConfig(),
		FETCH:    fetch.DefaultConfig(),
		LOGGING:  defaultLoggingConfig(),
		Sync:     syncer.DefaultConfig(),
		Recovery: checkpoint.DefaultConfig(),
	}
}
