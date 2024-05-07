// Package config contains go-spacemesh node configuration definitions
package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/bootstrap"
	"github.com/spacemeshos/go-spacemesh/checkpoint"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/fetch"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/syncer"
	timeConfig "github.com/spacemeshos/go-spacemesh/timesync/config"
	"github.com/spacemeshos/go-spacemesh/tortoise"
)

const (
	defaultDataDirName = "spacemesh"
	// NewBlockProtocol indicates the protocol name for new blocks arriving.
)

var (
	defaultHomeDir string
	defaultDataDir string
)

func init() {
	defaultHomeDir, _ = os.UserHomeDir()
	defaultDataDir = filepath.Join(defaultHomeDir, defaultDataDirName, "/")
}

// Config defines the top level configuration for a spacemesh node.
type Config struct {
	BaseConfig      `mapstructure:"main"`
	Preset          string                `mapstructure:"preset"`
	Genesis         GenesisConfig         `mapstructure:"genesis"`
	PublicMetrics   PublicMetrics         `mapstructure:"public-metrics"`
	Tortoise        tortoise.Config       `mapstructure:"tortoise"`
	P2P             p2p.Config            `mapstructure:"p2p"`
	API             grpcserver.Config     `mapstructure:"api"`
	HARE3           hare3.Config          `mapstructure:"hare3"`
	HareEligibility eligibility.Config    `mapstructure:"hare-eligibility"`
	Certificate     blocks.CertConfig     `mapstructure:"certificate"`
	Beacon          beacon.Config         `mapstructure:"beacon"`
	TIME            timeConfig.TimeConfig `mapstructure:"time"`
	VM              vm.Config             `mapstructure:"vm"`
	POST            activation.PostConfig `mapstructure:"post"`
	POSTService     activation.PostSupervisorConfig
	POET            activation.PoetConfig      `mapstructure:"poet"`
	SMESHING        SmeshingConfig             `mapstructure:"smeshing"`
	LOGGING         LoggerConfig               `mapstructure:"logging"`
	FETCH           fetch.Config               `mapstructure:"fetch"`
	Bootstrap       bootstrap.Config           `mapstructure:"bootstrap"`
	Sync            syncer.Config              `mapstructure:"syncer"`
	Recovery        checkpoint.Config          `mapstructure:"recovery"`
	Cache           datastore.Config           `mapstructure:"cache"`
	ActiveSet       miner.ActiveSetPreparation `mapstructure:"active-set-preparation"`
}

// DataDir returns the absolute path to use for the node's data. This is the tilde-expanded path given in the config
// with a subfolder named after the network ID.
func (cfg *Config) DataDir() string {
	return filepath.Clean(cfg.DataDirParent)
}

type TestConfig struct {
	SmesherKey string `mapstructure:"testing-smesher-key"`
}

// BaseConfig defines the default configuration options for spacemesh app.
type BaseConfig struct {
	DataDirParent string `mapstructure:"data-folder"`
	FileLock      string `mapstructure:"filelock"`

	TestConfig TestConfig `mapstructure:"testing"`
	Standalone bool       `mapstructure:"standalone"`

	CollectMetrics bool `mapstructure:"metrics"`
	MetricsPort    int  `mapstructure:"metrics-port"`

	ProfilerName string `mapstructure:"profiler-name"`
	ProfilerURL  string `mapstructure:"profiler-url"`

	LayerDuration  time.Duration `mapstructure:"layer-duration"`
	LayerAvgSize   uint32        `mapstructure:"layer-average-size"`
	LayersPerEpoch uint32        `mapstructure:"layers-per-epoch"`

	PoETServers DeprecatedPoETServers `mapstructure:"poet-server"`
	PoetServers []types.PoetServer    `mapstructure:"poet-servers"`

	PprofHTTPServer         bool   `mapstructure:"pprof-server"`
	PprofHTTPServerListener string `mapstructure:"pprof-listener"`

	TxsPerProposal int    `mapstructure:"txs-per-proposal"`
	BlockGasLimit  uint64 `mapstructure:"block-gas-limit"`
	// if the number of proposals with the same mesh state crosses this threshold (in percentage),
	// then we optimistically filter out infeasible transactions before constructing the block.
	OptFilterThreshold int    `mapstructure:"optimistic-filtering-threshold"`
	TickSize           uint64 `mapstructure:"tick-size"`

	DatabaseConnections          int                     `mapstructure:"db-connections"`
	DatabaseLatencyMetering      bool                    `mapstructure:"db-latency-metering"`
	DatabaseSizeMeteringInterval time.Duration           `mapstructure:"db-size-metering-interval"`
	DatabasePruneInterval        time.Duration           `mapstructure:"db-prune-interval"`
	DatabaseVacuumState          int                     `mapstructure:"db-vacuum-state"`
	DatabaseSkipMigrations       []int                   `mapstructure:"db-skip-migrations"`
	DatabaseQueryCache           bool                    `mapstructure:"db-query-cache"`
	DatabaseQueryCacheSizes      DatabaseQueryCacheSizes `mapstructure:"db-query-cache-sizes"`

	PruneActivesetsFrom types.EpochID `mapstructure:"prune-activesets-from"`

	// ScanMalfeasantATXs is a flag to enable scanning for malfeasant ATXs.
	ScanMalfeasantATXs bool `mapstructure:"scan-malfeasant-atxs"`

	NetworkHRP string `mapstructure:"network-hrp"`

	// MinerGoodAtxsPercent is a threshold to decide if tortoise activeset should be
	// picked from first block instead of synced data.
	MinerGoodAtxsPercent int `mapstructure:"miner-good-atxs-percent"`

	RegossipAtxInterval time.Duration `mapstructure:"regossip-atx-interval"`

	// ATXGradeDelay is used to grade ATXs for selection in tortoise active set.
	// See grading function in miner/proposals_builder.go
	ATXGradeDelay time.Duration `mapstructure:"atx-grade-delay"`

	// PostValidDelay is the time after which a PoST is considered valid
	// counting from the time an ATX was received.
	// Before that time, the PoST must be fully verified.
	// After that time, we depend on PoST malfeasance proofs.
	PostValidDelay time.Duration `mapstructure:"post-valid-delay"`

	// NoMainOverride forces the "nomain" builds to run on the mainnet
	NoMainOverride bool `mapstructure:"no-main-override"`
}

type DatabaseQueryCacheSizes struct {
	EpochATXs     int `mapstructure:"epoch-atxs"`
	ATXBlob       int `mapstructure:"atx-blob"`
	ActiveSetBlob int `mapstructure:"active-set-blob"`
}

type DeprecatedPoETServers struct{}

// DeprecatedMsg implements Deprecated interface.
func (DeprecatedPoETServers) DeprecatedMsg() string {
	return `The 'poet-server' is deprecated. Please migrate to the 'poet-servers'. ` +
		`Check 'Upgrade Information' in CHANGELOG.md for details.`
}

type PublicMetrics struct {
	MetricsURL        string            `mapstructure:"metrics-url"`
	MetricsPushPeriod time.Duration     `mapstructure:"metrics-push-period"`
	MetricsPushUser   string            `mapstructure:"metrics-push-user"`
	MetricsPushPass   string            `mapstructure:"metrics-push-pass"`
	MetricsPushHeader map[string]string `mapstructure:"metrics-push-header"`
}

// SmeshingConfig defines configuration for the node's smeshing (mining).
type SmeshingConfig struct {
	Start           bool                              `mapstructure:"smeshing-start"`
	CoinbaseAccount string                            `mapstructure:"smeshing-coinbase"`
	Opts            activation.PostSetupOpts          `mapstructure:"smeshing-opts"`
	ProvingOpts     activation.PostProvingOpts        `mapstructure:"smeshing-proving-opts"`
	VerifyingOpts   activation.PostProofVerifyingOpts `mapstructure:"smeshing-verifying-opts"`
}

// DefaultConfig returns the default configuration for a spacemesh node.
func DefaultConfig() Config {
	return Config{
		BaseConfig:      defaultBaseConfig(),
		Genesis:         DefaultGenesisConfig(),
		Tortoise:        tortoise.DefaultConfig(),
		P2P:             p2p.DefaultConfig(),
		API:             grpcserver.DefaultConfig(),
		HARE3:           hare3.DefaultConfig(),
		HareEligibility: eligibility.DefaultConfig(),
		Beacon:          beacon.DefaultConfig(),
		TIME:            timeConfig.DefaultConfig(),
		VM:              vm.DefaultConfig(),
		POST:            activation.DefaultPostConfig(),
		POSTService:     activation.DefaultPostServiceConfig(),
		POET:            activation.DefaultPoetConfig(),
		SMESHING:        DefaultSmeshingConfig(),
		FETCH:           fetch.DefaultConfig(),
		LOGGING:         DefaultLoggingConfig(),
		Bootstrap:       bootstrap.DefaultConfig(),
		Sync:            syncer.DefaultConfig(),
		Recovery:        checkpoint.DefaultConfig(),
		Cache:           datastore.DefaultConfig(),
		ActiveSet:       miner.DefaultActiveSetPreparation(),
	}
}

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig() Config {
	conf := DefaultConfig()
	conf.BaseConfig = defaultTestConfig()
	conf.Genesis = DefaultTestGenesisConfig()
	conf.P2P = p2p.DefaultConfig()
	conf.API = grpcserver.DefaultTestConfig()
	conf.POSTService = activation.DefaultTestPostServiceConfig()
	conf.HARE3.PreroundDelay = 1 * time.Second
	conf.HARE3.RoundDuration = 1 * time.Second
	return conf
}

// DefaultBaseConfig returns a default configuration for spacemesh.
func defaultBaseConfig() BaseConfig {
	return BaseConfig{
		DataDirParent:                defaultDataDir,
		FileLock:                     filepath.Join(os.TempDir(), "spacemesh.lock"),
		CollectMetrics:               false,
		MetricsPort:                  1010,
		ProfilerName:                 "go-spacemesh",
		LayerDuration:                30 * time.Second,
		LayersPerEpoch:               3,
		TxsPerProposal:               100,
		BlockGasLimit:                math.MaxUint64,
		OptFilterThreshold:           90,
		TickSize:                     100,
		DatabaseConnections:          16,
		DatabaseSizeMeteringInterval: 10 * time.Minute,
		DatabasePruneInterval:        30 * time.Minute,
		DatabaseQueryCacheSizes: DatabaseQueryCacheSizes{
			EpochATXs:     20,
			ATXBlob:       10000,
			ActiveSetBlob: 200,
		},
		NetworkHRP:     "sm",
		ATXGradeDelay:  10 * time.Second,
		PostValidDelay: 12 * time.Hour,

		PprofHTTPServerListener: "localhost:6060",
	}
}

// DefaultSmeshingConfig returns the node's default smeshing configuration.
func DefaultSmeshingConfig() SmeshingConfig {
	return SmeshingConfig{
		Start:           false,
		CoinbaseAccount: "",
		Opts:            activation.DefaultPostSetupOpts(),
		ProvingOpts:     activation.DefaultPostProvingOpts(),
		VerifyingOpts:   activation.DefaultPostVerifyingOpts(),
	}
}

func defaultTestConfig() BaseConfig {
	conf := defaultBaseConfig()
	conf.MetricsPort += 10000
	conf.NetworkHRP = "stest"
	types.SetNetworkHRP(conf.NetworkHRP)
	return conf
}

// LoadConfig load the config file.
func LoadConfig(config string, vip *viper.Viper) error {
	if config == "" {
		return nil
	}
	vip.SetConfigFile(config)
	if err := vip.ReadInConfig(); err != nil {
		return fmt.Errorf("can't load config at %s: %w", config, err)
	}
	return nil
}
