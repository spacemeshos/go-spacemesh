package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/cmd/flags"
	"github.com/spacemeshos/go-spacemesh/common/types"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
)

var config = cfg.DefaultConfig()

// AddCommands adds cobra commands to the app.
func AddCommands(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP("preset", "p", "",
		fmt.Sprintf("preset overwrites default values of the config. options %+s", presets.Options()))

	/** ======================== BaseConfig Flags ========================== **/
	cmd.PersistentFlags().StringVarP(&config.BaseConfig.ConfigFile,
		"config", "c", config.BaseConfig.ConfigFile, "Set Load configuration from file")
	cmd.PersistentFlags().StringVarP(&config.BaseConfig.DataDirParent, "data-folder", "d",
		config.BaseConfig.DataDirParent, "Specify data directory for spacemesh")
	cmd.PersistentFlags().StringVar(&config.LOGGING.Encoder, "log-encoder",
		config.LOGGING.Encoder, "Log as JSON instead of plain text")
	cmd.PersistentFlags().BoolVar(&config.CollectMetrics, "metrics",
		config.CollectMetrics, "collect node metrics")
	cmd.PersistentFlags().IntVar(&config.MetricsPort, "metrics-port",
		config.MetricsPort, "metric server port")
	cmd.PersistentFlags().StringVar(&config.MetricsPush, "metrics-push",
		config.MetricsPush, "Push metrics to url")
	cmd.PersistentFlags().IntVar(&config.MetricsPushPeriod, "metrics-push-period",
		config.MetricsPushPeriod, "Push period")
	cmd.PersistentFlags().StringVar(&config.OracleServer, "oracle_server",
		config.OracleServer, "The oracle server url. (temporary) ")
	cmd.PersistentFlags().IntVar(&config.OracleServerWorldID, "oracle_server_worldid",
		config.OracleServerWorldID, "The worldid to use with the oracle server (temporary) ")
	cmd.PersistentFlags().StringArrayVar(&config.PoETServers, "poet-server",
		config.PoETServers, "The poet server url. (temporary) Can be passed multiple times")
	cmd.PersistentFlags().StringVar(&config.Genesis.GenesisTime, "genesis-time",
		config.Genesis.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().IntVar(&config.LayerDurationSec, "layer-duration-sec",
		config.LayerDurationSec, "Duration between layers in seconds")
	cmd.PersistentFlags().IntVar(&config.LayerAvgSize, "layer-average-size",
		config.LayerAvgSize, "Layer Avg size")
	cmd.PersistentFlags().BoolVar(&config.PprofHTTPServer, "pprof-server",
		config.PprofHTTPServer, "enable http pprof server")
	cmd.PersistentFlags().Uint64Var(&config.TickSize, "tick-size", config.TickSize, "number of poet leaves in a single tick")
	cmd.PersistentFlags().StringVar(&config.PublishEventsURL, "events-url",
		config.PublishEventsURL, "publish events to this url; if no url specified no events will be published")
	cmd.PersistentFlags().StringVar(&config.ProfilerURL, "profiler-url",
		config.ProfilerURL, "send profiler data to certain url, if no url no profiling will be sent, format: http://<IP>:<PORT>")
	cmd.PersistentFlags().StringVar(&config.ProfilerName, "profiler-name",
		config.ProfilerName, "the name to use when sending profiles")

	cmd.PersistentFlags().IntVar(&config.SyncRequestTimeout, "sync-request-timeout",
		config.SyncRequestTimeout, "the timeout in ms for direct requests in the sync")
	cmd.PersistentFlags().IntVar(&config.TxsPerProposal, "txs-per-proposal",
		config.TxsPerProposal, "the number of transactions to select per proposal")
	cmd.PersistentFlags().Uint64Var(&config.BlockGasLimit, "block-gas-limit",
		config.BlockGasLimit, "max gas allowed per block")
	cmd.PersistentFlags().IntVar(&config.OptFilterThreshold, "optimistic-filtering-threshold",
		config.OptFilterThreshold, "threshold for optimistic filtering in percentage")

	cmd.PersistentFlags().VarP(flags.NewStringToUint64Value(config.Genesis.Accounts), "accounts", "a",
		"List of prefunded accounts")

	/** ======================== P2P Flags ========================== **/

	cmd.PersistentFlags().StringVar(&config.P2P.Listen, "listen",
		config.P2P.Listen, "address for listening")
	cmd.PersistentFlags().BoolVar(&config.P2P.Flood, "flood",
		config.P2P.Flood, "flood created messages to all peers (true by default. disable to lower traffic requirements)")
	cmd.PersistentFlags().BoolVar(&config.P2P.DisableNatPort, "disable-natport",
		config.P2P.DisableNatPort, "disable nat port-mapping (if enabled upnp protocol is used to negotiate external port with router)")
	cmd.PersistentFlags().IntVar(&config.P2P.LowPeers, "low-peers",
		config.P2P.LowPeers, "low watermark for the number of connections")
	cmd.PersistentFlags().IntVar(&config.P2P.HighPeers, "high-peers",
		config.P2P.HighPeers,
		"high watermark for the number of connections; once reached, connections are pruned until low watermark remains")
	cmd.PersistentFlags().IntVar(&config.P2P.TargetOutbound, "target-outbound",
		config.P2P.TargetOutbound, "target outbound connections")
	cmd.PersistentFlags().StringSliceVar(&config.P2P.Bootnodes, "bootnodes",
		config.P2P.Bootnodes, "entrypoints into the network")

	/** ======================== TIME Flags ========================== **/

	cmd.PersistentFlags().BoolVar(&config.TIME.Peersync.Disable, "peersync-disable", config.TIME.Peersync.Disable,
		"disable verification that local time is in sync with peers")
	cmd.PersistentFlags().DurationVar(&config.TIME.Peersync.RoundRetryInterval, "peersync-round-retry-interval",
		config.TIME.Peersync.RoundRetryInterval, "when to retry a sync round after a failure")
	cmd.PersistentFlags().DurationVar(&config.TIME.Peersync.RoundInterval, "peersync-round-interval",
		config.TIME.Peersync.RoundRetryInterval, "when to run a next sync round")
	cmd.PersistentFlags().DurationVar(&config.TIME.Peersync.RoundTimeout, "peersync-round-timeout",
		config.TIME.Peersync.RoundRetryInterval, "how long to wait for a round to complete")
	cmd.PersistentFlags().DurationVar(&config.TIME.Peersync.MaxClockOffset, "peersync-max-clock-offset",
		config.TIME.Peersync.MaxClockOffset, "max difference between local clock and peers clock")
	cmd.PersistentFlags().IntVar(&config.TIME.Peersync.MaxOffsetErrors, "peersync-max-offset-errors",
		config.TIME.Peersync.MaxOffsetErrors, "the node will exit when max number of consecutive offset errors will be reached")
	cmd.PersistentFlags().IntVar(&config.TIME.Peersync.RequiredResponses, "peersync-required-responses",
		config.TIME.Peersync.RequiredResponses, "min number of clock samples from other that need to be collected to verify time")
	/** ======================== API Flags ========================== **/

	// StartJSONServer determines if json api server should be started
	cmd.PersistentFlags().BoolVar(&config.API.StartJSONServer, "json-server",
		config.API.StartJSONServer, "Start the grpc-gateway (json http) server. "+
			"The gateway server will be enabled for all corresponding, enabled GRPC services.",
	)
	// JSONServerPort determines the json api server local listening port
	cmd.PersistentFlags().IntVar(&config.API.JSONServerPort, "json-port",
		config.API.JSONServerPort, "JSON api server port")
	// StartGrpcServices determines which (if any) GRPC API services should be started
	cmd.PersistentFlags().StringSliceVar(&config.API.StartGrpcServices, "grpc",
		config.API.StartGrpcServices, "Comma-separated list of individual grpc services to enable "+
			"(gateway,globalstate,mesh,node,smesher,transaction)")
	// GrpcServerPort determines the grpc server local listening port
	cmd.PersistentFlags().IntVar(&config.API.GrpcServerPort, "grpc-port",
		config.API.GrpcServerPort, "GRPC api server port")
	// GrpcServerInterface determines the interface the GRPC server listens on
	cmd.PersistentFlags().StringVar(&config.API.GrpcServerInterface, "grpc-interface",
		config.API.GrpcServerInterface, "GRPC api server interface")

	/**======================== Hare Flags ========================== **/

	// N determines the size of the hare committee
	cmd.PersistentFlags().IntVar(&config.HARE.N, "hare-committee-size",
		config.HARE.N, "Size of Hare committee")
	// F determines the max number of adversaries in the Hare committee
	cmd.PersistentFlags().IntVar(&config.HARE.F, "hare-max-adversaries",
		config.HARE.F, "Max number of adversaries in the Hare committee")
	// RoundDuration determines the duration of a round in the Hare protocol
	cmd.PersistentFlags().IntVar(&config.HARE.RoundDuration, "hare-round-duration-sec",
		config.HARE.RoundDuration, "Duration of round in the Hare protocol")
	cmd.PersistentFlags().IntVar(&config.HARE.WakeupDelta, "hare-wakeup-delta",
		config.HARE.WakeupDelta, "Wakeup delta after tick for hare protocol")
	cmd.PersistentFlags().IntVar(&config.HARE.ExpectedLeaders, "hare-exp-leaders",
		config.HARE.ExpectedLeaders, "The expected number of leaders in the hare protocol")
	cmd.PersistentFlags().IntVar(&config.HARE.LimitIterations, "hare-limit-iterations",
		config.HARE.LimitIterations, "The limit of the number of iteration per consensus process")
	cmd.PersistentFlags().IntVar(&config.HARE.LimitConcurrent, "hare-limit-concurrent",
		config.HARE.LimitConcurrent, "The number of consensus processes running concurrently")

	/**======================== Hare Eligibility Oracle Flags ========================== **/

	cmd.PersistentFlags().Uint32Var(&config.HareEligibility.ConfidenceParam, "eligibility-confidence-param",
		config.HareEligibility.ConfidenceParam, "The relative layer (with respect to the current layer) we are confident to have consensus about")
	cmd.PersistentFlags().Uint32Var(&config.HareEligibility.EpochOffset, "eligibility-epoch-offset",
		config.HareEligibility.EpochOffset, "The constant layer (within an epoch) for which we traverse its view for the purpose of counting consensus active set")

	/**======================== Beacon Flags ========================== **/

	cmd.PersistentFlags().Uint64Var(&config.Beacon.Kappa, "beacon-kappa",
		config.Beacon.Kappa, "Security parameter (for calculating ATX threshold)")
	cmd.PersistentFlags().Var((*types.RatVar)(config.Beacon.Q), "beacon-q",
		"Ratio of dishonest spacetime (for calculating ATX threshold). It should be a string representing a rational number.")
	cmd.PersistentFlags().Uint32Var((*uint32)(&config.Beacon.RoundsNumber), "beacon-rounds-number",
		uint32(config.Beacon.RoundsNumber), "Amount of rounds in every epoch")
	cmd.PersistentFlags().DurationVar(&config.Beacon.GracePeriodDuration, "beacon-grace-period-duration",
		config.Beacon.GracePeriodDuration, "Grace period duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&config.Beacon.ProposalDuration, "beacon-proposal-duration",
		config.Beacon.ProposalDuration, "Proposal duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&config.Beacon.FirstVotingRoundDuration, "beacon-first-voting-round-duration",
		config.Beacon.FirstVotingRoundDuration, "First voting round duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&config.Beacon.VotingRoundDuration, "beacon-voting-round-duration",
		config.Beacon.VotingRoundDuration, "Voting round duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&config.Beacon.WeakCoinRoundDuration, "beacon-weak-coin-round-duration",
		config.Beacon.WeakCoinRoundDuration, "Weak coin round duration in milliseconds")
	cmd.PersistentFlags().Var((*types.RatVar)(config.Beacon.Theta), "beacon-theta",
		"Ratio of votes for reaching consensus")
	cmd.PersistentFlags().Uint32Var(&config.Beacon.VotesLimit, "beacon-votes-limit",
		config.Beacon.VotesLimit, "Maximum allowed number of votes to be sent")
	cmd.PersistentFlags().Uint32Var(&config.Beacon.BeaconSyncNumBallots, "beacon-sync-num-blocks",
		config.Beacon.BeaconSyncNumBallots, "Numbers of blocks to wait before determining beacon values from them.")

	/**======================== Tortoise Flags ========================== **/
	cmd.PersistentFlags().Uint32Var(&config.Tortoise.Hdist, "tortoise-hdist",
		config.Tortoise.Hdist, "hdist")
	cmd.PersistentFlags().DurationVar(&config.Tortoise.RerunInterval, "tortoise-rerun-interval",
		config.Tortoise.RerunInterval, "Tortoise will verify layers from scratch every interval.")

	// TODO(moshababo): add usage desc

	cmd.PersistentFlags().UintVar(&config.POST.BitsPerLabel, "post-bits-per-label",
		config.POST.BitsPerLabel, "")
	cmd.PersistentFlags().UintVar(&config.POST.LabelsPerUnit, "post-labels-per-unit",
		config.POST.LabelsPerUnit, "")
	cmd.PersistentFlags().UintVar(&config.POST.MinNumUnits, "post-min-numunits",
		config.POST.MinNumUnits, "")
	cmd.PersistentFlags().UintVar(&config.POST.MaxNumUnits, "post-max-numunits",
		config.POST.MaxNumUnits, "")
	cmd.PersistentFlags().UintVar(&config.POST.K1, "post-k1",
		config.POST.K1, "")
	cmd.PersistentFlags().UintVar(&config.POST.K2, "post-k2",
		config.POST.K2, "")

	/**======================== Smeshing Flags ========================== **/

	// TODO(moshababo): add usage desc

	cmd.PersistentFlags().BoolVar(&config.SMESHING.Start, "smeshing-start",
		config.SMESHING.Start, "")
	cmd.PersistentFlags().StringVar(&config.SMESHING.CoinbaseAccount, "smeshing-coinbase",
		config.SMESHING.CoinbaseAccount, "coinbase account to accumulate rewards")
	cmd.PersistentFlags().StringVar(&config.SMESHING.Opts.DataDir, "smeshing-opts-datadir",
		config.SMESHING.Opts.DataDir, "")
	cmd.PersistentFlags().UintVar(&config.SMESHING.Opts.NumUnits, "smeshing-opts-numunits",
		config.SMESHING.Opts.NumUnits, "")
	cmd.PersistentFlags().UintVar(&config.SMESHING.Opts.NumFiles, "smeshing-opts-numfiles",
		config.SMESHING.Opts.NumFiles, "")
	cmd.PersistentFlags().IntVar(&config.SMESHING.Opts.ComputeProviderID, "smeshing-opts-provider",
		config.SMESHING.Opts.ComputeProviderID, "")
	cmd.PersistentFlags().BoolVar(&config.SMESHING.Opts.Throttle, "smeshing-opts-throttle",
		config.SMESHING.Opts.Throttle, "")

	/**======================== Consensus Flags ========================== **/

	cmd.PersistentFlags().Uint32Var(&config.LayersPerEpoch, "layers-per-epoch",
		config.LayersPerEpoch, "number of layers in epoch")

	/**======================== PoET Flags ========================== **/

	cmd.PersistentFlags().DurationVar(&config.POET.PhaseShift, "phase-shift",
		config.POET.PhaseShift, "phase shift of poet server")
	cmd.PersistentFlags().DurationVar(&config.POET.CycleGap, "cycle-gap",
		config.POET.CycleGap, "cycle gap of poet server")
	cmd.PersistentFlags().DurationVar(&config.POET.GracePeriod, "grace-period",
		config.POET.GracePeriod, "propagation time for ATXs in the network")

	// Bind Flags to config
	err := viper.BindPFlags(cmd.PersistentFlags())
	if err != nil {
		fmt.Println("an error has occurred while binding flags:", err)
	}
}
