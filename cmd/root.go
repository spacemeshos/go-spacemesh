package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/cmd/flags"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
)

var cfg = config.DefaultConfig()

// AddCommands adds cobra commands to the app.
func AddCommands(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP("preset", "p", "",
		fmt.Sprintf("preset overwrites default values of the config. options %+s", presets.Options()))

	/** ======================== BaseConfig Flags ========================== **/
	cmd.PersistentFlags().StringVarP(&cfg.BaseConfig.ConfigFile,
		"config", "c", cfg.BaseConfig.ConfigFile, "Set Load configuration from file")
	cmd.PersistentFlags().StringVarP(&cfg.BaseConfig.DataDirParent, "data-folder", "d",
		cfg.BaseConfig.DataDirParent, "Specify data directory for spacemesh")
	cmd.PersistentFlags().StringVar(&cfg.LOGGING.Encoder, "log-encoder",
		cfg.LOGGING.Encoder, "Log as JSON instead of plain text")
	cmd.PersistentFlags().BoolVar(&cfg.CollectMetrics, "metrics",
		cfg.CollectMetrics, "collect node metrics")
	cmd.PersistentFlags().IntVar(&cfg.MetricsPort, "metrics-port",
		cfg.MetricsPort, "metric server port")
	cmd.PersistentFlags().StringVar(&cfg.MetricsPush, "metrics-push",
		cfg.MetricsPush, "Push metrics to url")
	cmd.PersistentFlags().IntVar(&cfg.MetricsPushPeriod, "metrics-push-period",
		cfg.MetricsPushPeriod, "Push period")
	cmd.PersistentFlags().StringVar(&cfg.OracleServer, "oracle_server",
		cfg.OracleServer, "The oracle server url. (temporary) ")
	cmd.PersistentFlags().IntVar(&cfg.OracleServerWorldID, "oracle_server_worldid",
		cfg.OracleServerWorldID, "The worldid to use with the oracle server (temporary) ")
	cmd.PersistentFlags().StringArrayVar(&cfg.PoETServers, "poet-server",
		cfg.PoETServers, "The poet server url. (temporary) Can be passed multiple times")
	cmd.PersistentFlags().StringVar(&cfg.Genesis.GenesisTime, "genesis-time",
		cfg.Genesis.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().StringVar(&cfg.Genesis.ExtraData, "genesis-extra-data",
		cfg.Genesis.ExtraData, "genesis extra-data will be committed to the genesis id")
	cmd.PersistentFlags().IntVar(&cfg.LayerDurationSec, "layer-duration-sec",
		cfg.LayerDurationSec, "Duration between layers in seconds")
	cmd.PersistentFlags().IntVar(&cfg.LayerAvgSize, "layer-average-size",
		cfg.LayerAvgSize, "Layer Avg size")
	cmd.PersistentFlags().BoolVar(&cfg.PprofHTTPServer, "pprof-server",
		cfg.PprofHTTPServer, "enable http pprof server")
	cmd.PersistentFlags().Uint64Var(&cfg.TickSize, "tick-size", cfg.TickSize, "number of poet leaves in a single tick")
	cmd.PersistentFlags().StringVar(&cfg.PublishEventsURL, "events-url",
		cfg.PublishEventsURL, "publish events to this url; if no url specified no events will be published")
	cmd.PersistentFlags().StringVar(&cfg.ProfilerURL, "profiler-url",
		cfg.ProfilerURL, "send profiler data to certain url, if no url no profiling will be sent, format: http://<IP>:<PORT>")
	cmd.PersistentFlags().StringVar(&cfg.ProfilerName, "profiler-name",
		cfg.ProfilerName, "the name to use when sending profiles")

	cmd.PersistentFlags().IntVar(&cfg.SyncRequestTimeout, "sync-request-timeout",
		cfg.SyncRequestTimeout, "the timeout in ms for direct requests in the sync")
	cmd.PersistentFlags().IntVar(&cfg.TxsPerProposal, "txs-per-proposal",
		cfg.TxsPerProposal, "the number of transactions to select per proposal")
	cmd.PersistentFlags().Uint64Var(&cfg.BlockGasLimit, "block-gas-limit",
		cfg.BlockGasLimit, "max gas allowed per block")
	cmd.PersistentFlags().IntVar(&cfg.OptFilterThreshold, "optimistic-filtering-threshold",
		cfg.OptFilterThreshold, "threshold for optimistic filtering in percentage")

	cmd.PersistentFlags().VarP(flags.NewStringToUint64Value(cfg.Genesis.Accounts), "accounts", "a",
		"List of prefunded accounts")

	/** ======================== P2P Flags ========================== **/

	cmd.PersistentFlags().StringVar(&cfg.P2P.Listen, "listen",
		cfg.P2P.Listen, "address for listening")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.Flood, "flood",
		cfg.P2P.Flood, "flood created messages to all peers (true by default. disable to lower traffic requirements)")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.DisableNatPort, "disable-natport",
		cfg.P2P.DisableNatPort, "disable nat port-mapping (if enabled upnp protocol is used to negotiate external port with router)")
	cmd.PersistentFlags().IntVar(&cfg.P2P.LowPeers, "low-peers",
		cfg.P2P.LowPeers, "low watermark for the number of connections")
	cmd.PersistentFlags().IntVar(&cfg.P2P.HighPeers, "high-peers",
		cfg.P2P.HighPeers,
		"high watermark for the number of connections; once reached, connections are pruned until low watermark remains")
	cmd.PersistentFlags().IntVar(&cfg.P2P.TargetOutbound, "target-outbound",
		cfg.P2P.TargetOutbound, "target outbound connections")
	cmd.PersistentFlags().StringSliceVar(&cfg.P2P.Bootnodes, "bootnodes",
		cfg.P2P.Bootnodes, "entrypoints into the network")
	cmd.PersistentFlags().StringVar(&cfg.P2P.AdvertiseAddress, "advertise-address",
		cfg.P2P.AdvertiseAddress, "libp2p address with identity (example: /dns4/bootnode.spacemesh.io/tcp/5003)")

	/** ======================== TIME Flags ========================== **/

	cmd.PersistentFlags().BoolVar(&cfg.TIME.Peersync.Disable, "peersync-disable", cfg.TIME.Peersync.Disable,
		"disable verification that local time is in sync with peers")
	cmd.PersistentFlags().DurationVar(&cfg.TIME.Peersync.RoundRetryInterval, "peersync-round-retry-interval",
		cfg.TIME.Peersync.RoundRetryInterval, "when to retry a sync round after a failure")
	cmd.PersistentFlags().DurationVar(&cfg.TIME.Peersync.RoundInterval, "peersync-round-interval",
		cfg.TIME.Peersync.RoundRetryInterval, "when to run a next sync round")
	cmd.PersistentFlags().DurationVar(&cfg.TIME.Peersync.RoundTimeout, "peersync-round-timeout",
		cfg.TIME.Peersync.RoundRetryInterval, "how long to wait for a round to complete")
	cmd.PersistentFlags().DurationVar(&cfg.TIME.Peersync.MaxClockOffset, "peersync-max-clock-offset",
		cfg.TIME.Peersync.MaxClockOffset, "max difference between local clock and peers clock")
	cmd.PersistentFlags().IntVar(&cfg.TIME.Peersync.MaxOffsetErrors, "peersync-max-offset-errors",
		cfg.TIME.Peersync.MaxOffsetErrors, "the node will exit when max number of consecutive offset errors will be reached")
	cmd.PersistentFlags().IntVar(&cfg.TIME.Peersync.RequiredResponses, "peersync-required-responses",
		cfg.TIME.Peersync.RequiredResponses, "min number of clock samples from other that need to be collected to verify time")
	/** ======================== API Flags ========================== **/

	// StartJSONServer determines if json api server should be started
	cmd.PersistentFlags().BoolVar(&cfg.API.StartJSONServer, "json-server",
		cfg.API.StartJSONServer, "Start the grpc-gateway (json http) server. "+
			"The gateway server will be enabled for all corresponding, enabled GRPC services.",
	)
	// JSONServerPort determines the json api server local listening port
	cmd.PersistentFlags().IntVar(&cfg.API.JSONServerPort, "json-port",
		cfg.API.JSONServerPort, "JSON api server port")
	// StartGrpcServices determines which (if any) GRPC API services should be started
	cmd.PersistentFlags().StringSliceVar(&cfg.API.StartGrpcServices, "grpc",
		cfg.API.StartGrpcServices, "Comma-separated list of individual grpc services to enable "+
			"(gateway,globalstate,mesh,node,smesher,transaction)")
	// GrpcServerPort determines the grpc server local listening port
	cmd.PersistentFlags().IntVar(&cfg.API.GrpcServerPort, "grpc-port",
		cfg.API.GrpcServerPort, "GRPC api server port")
	// GrpcServerInterface determines the interface the GRPC server listens on
	cmd.PersistentFlags().StringVar(&cfg.API.GrpcServerInterface, "grpc-interface",
		cfg.API.GrpcServerInterface, "GRPC api server interface")

	/**======================== Hare Flags ========================== **/

	// N determines the size of the hare committee
	cmd.PersistentFlags().IntVar(&cfg.HARE.N, "hare-committee-size",
		cfg.HARE.N, "Size of Hare committee")
	// F determines the max number of adversaries in the Hare committee
	cmd.PersistentFlags().IntVar(&cfg.HARE.F, "hare-max-adversaries",
		cfg.HARE.F, "Max number of adversaries in the Hare committee")
	// RoundDuration determines the duration of a round in the Hare protocol
	cmd.PersistentFlags().IntVar(&cfg.HARE.RoundDuration, "hare-round-duration-sec",
		cfg.HARE.RoundDuration, "Duration of round in the Hare protocol")
	cmd.PersistentFlags().IntVar(&cfg.HARE.WakeupDelta, "hare-wakeup-delta",
		cfg.HARE.WakeupDelta, "Wakeup delta after tick for hare protocol")
	cmd.PersistentFlags().IntVar(&cfg.HARE.ExpectedLeaders, "hare-exp-leaders",
		cfg.HARE.ExpectedLeaders, "The expected number of leaders in the hare protocol")
	cmd.PersistentFlags().IntVar(&cfg.HARE.LimitIterations, "hare-limit-iterations",
		cfg.HARE.LimitIterations, "The limit of the number of iteration per consensus process")
	cmd.PersistentFlags().IntVar(&cfg.HARE.LimitConcurrent, "hare-limit-concurrent",
		cfg.HARE.LimitConcurrent, "The number of consensus processes running concurrently")

	/**======================== Hare Eligibility Oracle Flags ========================== **/

	cmd.PersistentFlags().Uint32Var(&cfg.HareEligibility.ConfidenceParam, "eligibility-confidence-param",
		cfg.HareEligibility.ConfidenceParam, "The relative layer (with respect to the current layer) we are confident to have consensus about")
	cmd.PersistentFlags().Uint32Var(&cfg.HareEligibility.EpochOffset, "eligibility-epoch-offset",
		cfg.HareEligibility.EpochOffset, "The constant layer (within an epoch) for which we traverse its view for the purpose of counting consensus active set")

	/**======================== Beacon Flags ========================== **/

	cmd.PersistentFlags().IntVar(&cfg.Beacon.Kappa, "beacon-kappa",
		cfg.Beacon.Kappa, "Security parameter (for calculating ATX threshold)")
	cmd.PersistentFlags().Var((*types.RatVar)(cfg.Beacon.Q), "beacon-q",
		"Ratio of dishonest spacetime (for calculating ATX threshold). It should be a string representing a rational number.")
	cmd.PersistentFlags().Uint32Var((*uint32)(&cfg.Beacon.RoundsNumber), "beacon-rounds-number",
		uint32(cfg.Beacon.RoundsNumber), "Amount of rounds in every epoch")
	cmd.PersistentFlags().DurationVar(&cfg.Beacon.GracePeriodDuration, "beacon-grace-period-duration",
		cfg.Beacon.GracePeriodDuration, "Grace period duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&cfg.Beacon.ProposalDuration, "beacon-proposal-duration",
		cfg.Beacon.ProposalDuration, "Proposal duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&cfg.Beacon.FirstVotingRoundDuration, "beacon-first-voting-round-duration",
		cfg.Beacon.FirstVotingRoundDuration, "First voting round duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&cfg.Beacon.VotingRoundDuration, "beacon-voting-round-duration",
		cfg.Beacon.VotingRoundDuration, "Voting round duration in milliseconds")
	cmd.PersistentFlags().DurationVar(&cfg.Beacon.WeakCoinRoundDuration, "beacon-weak-coin-round-duration",
		cfg.Beacon.WeakCoinRoundDuration, "Weak coin round duration in milliseconds")
	cmd.PersistentFlags().Var((*types.RatVar)(cfg.Beacon.Theta), "beacon-theta",
		"Ratio of votes for reaching consensus")
	cmd.PersistentFlags().Uint32Var(&cfg.Beacon.VotesLimit, "beacon-votes-limit",
		cfg.Beacon.VotesLimit, "Maximum allowed number of votes to be sent")
	cmd.PersistentFlags().IntVar(&cfg.Beacon.BeaconSyncWeightUnits, "beacon-sync-weight-units",
		cfg.Beacon.BeaconSyncWeightUnits, "Numbers of weight units to wait before determining beacon values from them.")

	/**======================== Tortoise Flags ========================== **/
	cmd.PersistentFlags().Uint32Var(&cfg.Tortoise.Hdist, "tortoise-hdist",
		cfg.Tortoise.Hdist, "hdist")

	// TODO(moshababo): add usage desc

	cmd.PersistentFlags().Uint8Var(&cfg.POST.BitsPerLabel, "post-bits-per-label",
		cfg.POST.BitsPerLabel, "")
	cmd.PersistentFlags().Uint64Var(&cfg.POST.LabelsPerUnit, "post-labels-per-unit",
		cfg.POST.LabelsPerUnit, "")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.MinNumUnits, "post-min-numunits",
		cfg.POST.MinNumUnits, "")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.MaxNumUnits, "post-max-numunits",
		cfg.POST.MaxNumUnits, "")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.K1, "post-k1",
		cfg.POST.K1, "")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.K2, "post-k2",
		cfg.POST.K2, "")

	/**======================== Smeshing Flags ========================== **/

	// TODO(moshababo): add usage desc

	cmd.PersistentFlags().BoolVar(&cfg.SMESHING.Start, "smeshing-start",
		cfg.SMESHING.Start, "")
	cmd.PersistentFlags().StringVar(&cfg.SMESHING.CoinbaseAccount, "smeshing-coinbase",
		cfg.SMESHING.CoinbaseAccount, "coinbase account to accumulate rewards")
	cmd.PersistentFlags().StringVar(&cfg.SMESHING.Opts.DataDir, "smeshing-opts-datadir",
		cfg.SMESHING.Opts.DataDir, "")
	cmd.PersistentFlags().Uint32Var(&cfg.SMESHING.Opts.NumUnits, "smeshing-opts-numunits",
		cfg.SMESHING.Opts.NumUnits, "")
	cmd.PersistentFlags().Uint64Var(&cfg.SMESHING.Opts.MaxFileSize, "smeshing-opts-maxfilesize",
		cfg.SMESHING.Opts.MaxFileSize, "")
	cmd.PersistentFlags().IntVar(&cfg.SMESHING.Opts.ComputeProviderID, "smeshing-opts-provider",
		cfg.SMESHING.Opts.ComputeProviderID, "")
	cmd.PersistentFlags().BoolVar(&cfg.SMESHING.Opts.Throttle, "smeshing-opts-throttle",
		cfg.SMESHING.Opts.Throttle, "")

	/**======================== Consensus Flags ========================== **/

	cmd.PersistentFlags().Uint32Var(&cfg.LayersPerEpoch, "layers-per-epoch",
		cfg.LayersPerEpoch, "number of layers in epoch")

	/**======================== PoET Flags ========================== **/

	cmd.PersistentFlags().DurationVar(&cfg.POET.PhaseShift, "phase-shift",
		cfg.POET.PhaseShift, "phase shift of poet server")
	cmd.PersistentFlags().DurationVar(&cfg.POET.CycleGap, "cycle-gap",
		cfg.POET.CycleGap, "cycle gap of poet server")
	cmd.PersistentFlags().DurationVar(&cfg.POET.GracePeriod, "grace-period",
		cfg.POET.GracePeriod, "propagation time for ATXs in the network")

	// Bind Flags to config
	err := viper.BindPFlags(cmd.PersistentFlags())
	if err != nil {
		fmt.Println("an error has occurred while binding flags:", err)
	}
}
