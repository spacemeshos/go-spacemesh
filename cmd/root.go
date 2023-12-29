package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/node/flags"
)

var cfg = config.MainnetConfig()

func ResetConfig() {
	cfg = config.MainnetConfig()
}

// AddCommands adds cobra commands to the app.
func AddCommands(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP("preset", "p", "",
		fmt.Sprintf("preset overwrites default values of the config. options %+s", presets.Options()))

	/** ======================== Checkpoint Flags ========================== **/
	cmd.PersistentFlags().StringVar(&cfg.Recovery.Uri,
		"recovery-uri", cfg.Recovery.Uri, "reset the node state based on the supplied checkpoint file")
	cmd.PersistentFlags().Uint32Var(&cfg.Recovery.Restore,
		"recovery-layer", cfg.Recovery.Restore, "restart the mesh with the checkpoint file at this layer")

	/** ======================== BaseConfig Flags ========================== **/
	cmd.PersistentFlags().StringVarP(&cfg.BaseConfig.ConfigFile,
		"config", "c", cfg.BaseConfig.ConfigFile, "Set Load configuration from file")
	cmd.PersistentFlags().StringVarP(&cfg.BaseConfig.DataDirParent, "data-folder", "d",
		cfg.BaseConfig.DataDirParent, "Specify data directory for spacemesh")
	cmd.PersistentFlags().StringVar(&cfg.BaseConfig.FileLock,
		"filelock", cfg.BaseConfig.FileLock, "Filesystem lock to prevent running more than one instance.")
	cmd.PersistentFlags().StringVar(&cfg.LOGGING.Encoder, "log-encoder",
		cfg.LOGGING.Encoder, "Log as JSON instead of plain text")
	cmd.PersistentFlags().BoolVar(&cfg.CollectMetrics, "metrics",
		cfg.CollectMetrics, "collect node metrics")
	cmd.PersistentFlags().IntVar(&cfg.MetricsPort, "metrics-port",
		cfg.MetricsPort, "metric server port")
	cmd.PersistentFlags().StringVar(&cfg.PublicMetrics.MetricsURL, "metrics-push",
		cfg.PublicMetrics.MetricsURL, "Push metrics to url")
	cmd.PersistentFlags().DurationVar(&cfg.PublicMetrics.MetricsPushPeriod, "metrics-push-period",
		cfg.PublicMetrics.MetricsPushPeriod, "Push period")
	cmd.PersistentFlags().StringArrayVar(&cfg.PoETServers, "poet-server",
		cfg.PoETServers, "The poet server url. (temporary) Can be passed multiple times")
	cmd.PersistentFlags().StringVar(&cfg.Genesis.GenesisTime, "genesis-time",
		cfg.Genesis.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().StringVar(&cfg.Genesis.ExtraData, "genesis-extra-data",
		cfg.Genesis.ExtraData, "genesis extra-data will be committed to the genesis id")
	cmd.PersistentFlags().DurationVar(&cfg.LayerDuration, "layer-duration",
		cfg.LayerDuration, "Duration between layers")
	cmd.PersistentFlags().Uint32Var(&cfg.LayerAvgSize, "layer-average-size",
		cfg.LayerAvgSize, "Layer Avg size")
	cmd.PersistentFlags().BoolVar(&cfg.PprofHTTPServer, "pprof-server",
		cfg.PprofHTTPServer, "enable http pprof server")
	cmd.PersistentFlags().Uint64Var(&cfg.TickSize, "tick-size", cfg.TickSize, "number of poet leaves in a single tick")
	cmd.PersistentFlags().StringVar(&cfg.ProfilerURL, "profiler-url",
		cfg.ProfilerURL, "send profiler data to certain url, if no url no profiling will be sent, format: http://<IP>:<PORT>")
	cmd.PersistentFlags().StringVar(&cfg.ProfilerName, "profiler-name",
		cfg.ProfilerName, "the name to use when sending profiles")

	cmd.PersistentFlags().IntVar(&cfg.TxsPerProposal, "txs-per-proposal",
		cfg.TxsPerProposal, "the number of transactions to select per proposal")
	cmd.PersistentFlags().Uint64Var(&cfg.BlockGasLimit, "block-gas-limit",
		cfg.BlockGasLimit, "max gas allowed per block")
	cmd.PersistentFlags().IntVar(&cfg.OptFilterThreshold, "optimistic-filtering-threshold",
		cfg.OptFilterThreshold, "threshold for optimistic filtering in percentage")

	cmd.PersistentFlags().VarP(flags.NewStringToUint64Value(map[string]uint64{}), "accounts", "a",
		"List of prefunded accounts")

	cmd.PersistentFlags().IntVar(&cfg.DatabaseConnections, "db-connections",
		cfg.DatabaseConnections, "configure number of active connections to enable parallel read requests")
	cmd.PersistentFlags().BoolVar(&cfg.DatabaseLatencyMetering, "db-latency-metering",
		cfg.DatabaseLatencyMetering, "if enabled collect latency histogram for every database query")
	cmd.PersistentFlags().DurationVar(&cfg.DatabasePruneInterval, "db-prune-interval",
		cfg.DatabasePruneInterval, "configure interval for database pruning")

	/** ======================== P2P Flags ========================== **/

	cmd.PersistentFlags().Var(flags.NewAddressListValue(cfg.P2P.Listen, &cfg.P2P.Listen),
		"listen", "address(es) for listening")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.Flood, "flood",
		cfg.P2P.Flood, "flood created messages to all peers")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.DisableNatPort, "disable-natport",
		cfg.P2P.DisableNatPort, "disable nat port-mapping (if enabled upnp protocol is used to negotiate external port with router)")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.DisableReusePort,
		"disable-reuseport",
		cfg.P2P.DisableReusePort,
		"disables SO_REUSEPORT for tcp sockets. Try disabling this if your node can't reach bootnodes in the network",
	)
	cmd.PersistentFlags().BoolVar(&cfg.P2P.Metrics,
		"p2p-metrics",
		cfg.P2P.Metrics,
		"enable extended metrics collection from libp2p components",
	)
	cmd.PersistentFlags().IntVar(&cfg.P2P.AcceptQueue,
		"p2p-accept-queue",
		cfg.P2P.AcceptQueue,
		"number of connections that are fully setup before accepting new connections",
	)
	cmd.PersistentFlags().IntVar(&cfg.P2P.LowPeers, "low-peers",
		cfg.P2P.LowPeers, "low watermark for the number of connections")
	cmd.PersistentFlags().IntVar(&cfg.P2P.HighPeers, "high-peers",
		cfg.P2P.HighPeers,
		"high watermark for the number of connections; once reached, connections are pruned until low watermark remains")
	cmd.PersistentFlags().IntVar(&cfg.P2P.MinPeers, "min-peers",
		cfg.P2P.MinPeers, "actively search for peers until you get this much")
	cmd.PersistentFlags().StringSliceVar(&cfg.P2P.Bootnodes, "bootnodes",
		cfg.P2P.Bootnodes, "entrypoints into the network")
	cmd.PersistentFlags().StringSliceVar(&cfg.P2P.PingPeers, "ping-peers", cfg.P2P.Bootnodes, "peers to ping")
	cmd.PersistentFlags().DurationVar(&cfg.P2P.PingInterval, "ping-interval", cfg.P2P.PingInterval, "ping interval")
	cmd.PersistentFlags().StringSliceVar(&cfg.P2P.StaticRelays, "static-relays",
		cfg.P2P.StaticRelays, "static relay list")
	cmd.PersistentFlags().Var(flags.NewAddressListValue(cfg.P2P.AdvertiseAddress, &cfg.P2P.AdvertiseAddress),
		"advertise-address",
		"libp2p address(es) with identity (example: /dns4/bootnode.spacemesh.io/tcp/5003)")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.Bootnode, "p2p-bootnode", cfg.P2P.Bootnode,
		"gossipsub and discovery will be running in a mode suitable for bootnode")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.PrivateNetwork, "p2p-private-network", cfg.P2P.PrivateNetwork,
		"discovery will work in private mode. mostly useful for testing, don't set in public networks")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.ForceDHTServer, "force-dht-server", cfg.P2P.ForceDHTServer,
		"force DHT server mode")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.EnableTCPTransport, "enable-tcp-transport", cfg.P2P.EnableTCPTransport,
		"enable TCP transport")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.EnableQUICTransport, "enable-quic-transport", cfg.P2P.EnableQUICTransport,
		"enable QUIC transport")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.EnableRoutingDiscovery, "enable-routing-discovery", cfg.P2P.EnableQUICTransport,
		"enable routing discovery")
	cmd.PersistentFlags().BoolVar(&cfg.P2P.RoutingDiscoveryAdvertise, "routing-discovery-advertise",
		cfg.P2P.RoutingDiscoveryAdvertise, "advertise for routing discovery")

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

	cmd.PersistentFlags().StringSliceVar(&cfg.API.PublicServices, "grpc-public-services",
		cfg.API.PublicServices, "List of services that are safe to open for the network.")
	cmd.PersistentFlags().StringVar(&cfg.API.PublicListener, "grpc-public-listener",
		cfg.API.PublicListener, "Socket for the list of services specified in grpc-public-services.")
	cmd.PersistentFlags().StringSliceVar(&cfg.API.PrivateServices, "grpc-private-services",
		cfg.API.PrivateServices, "List of services that must be kept private or exposed only in secure environments.")
	cmd.PersistentFlags().StringVar(&cfg.API.PrivateListener, "grpc-private-listener",
		cfg.API.PrivateListener, "Socket for the list of services specified in grpc-private-services.")
	cmd.PersistentFlags().IntVar(&cfg.API.GrpcRecvMsgSize, "grpc-recv-msg-size",
		cfg.API.GrpcRecvMsgSize, "GRPC api recv message size")
	cmd.PersistentFlags().IntVar(&cfg.API.GrpcSendMsgSize, "grpc-send-msg-size",
		cfg.API.GrpcSendMsgSize, "GRPC api send message size")
	cmd.PersistentFlags().StringVar(&cfg.API.JSONListener, "grpc-json-listener",
		cfg.API.JSONListener, "Socket for the grpc gateway for the list of services in grpc-public-services. If left empty - grpc gateway won't be enabled.")
	/**======================== Hare Flags ========================== **/

	// N determines the size of the hare committee
	cmd.PersistentFlags().IntVar(&cfg.HARE.N, "hare-committee-size",
		cfg.HARE.N, "Size of Hare committee")
	// RoundDuration determines the duration of a round in the Hare protocol
	cmd.PersistentFlags().DurationVar(&cfg.HARE.RoundDuration, "hare-round-duration",
		cfg.HARE.RoundDuration, "Duration of round in the Hare protocol")
	cmd.PersistentFlags().DurationVar(&cfg.HARE.WakeupDelta, "hare-wakeup-delta",
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
		cfg.Tortoise.Hdist, "the distance for tortoise to vote according to hare output")
	cmd.PersistentFlags().Uint32Var(&cfg.Tortoise.Zdist, "tortoise-zdist",
		cfg.Tortoise.Zdist, "the distance for tortoise to wait for hare output")
	cmd.PersistentFlags().Uint32Var(&cfg.Tortoise.WindowSize, "tortoise-window-size",
		cfg.Tortoise.WindowSize, "size of the tortoise sliding window in layers")
	cmd.PersistentFlags().IntVar(&cfg.Tortoise.MaxExceptions, "tortoise-max-exceptions",
		cfg.Tortoise.MaxExceptions, "number of exceptions tolerated for a base ballot")
	cmd.PersistentFlags().Uint32Var(&cfg.Tortoise.BadBeaconVoteDelayLayers, "tortoise-delay-layers",
		cfg.Tortoise.BadBeaconVoteDelayLayers, "number of layers to ignore a ballot with a different beacon")
	cmd.PersistentFlags().BoolVar(&cfg.Tortoise.EnableTracer, "tortoise-enable-tracer",
		cfg.Tortoise.EnableTracer, "recovrd every tortoise input/output into the loggin output")

	// TODO(moshababo): add usage desc
	cmd.PersistentFlags().Uint64Var(&cfg.POST.LabelsPerUnit, "post-labels-per-unit",
		cfg.POST.LabelsPerUnit, "")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.MinNumUnits, "post-min-numunits",
		cfg.POST.MinNumUnits, "")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.MaxNumUnits, "post-max-numunits",
		cfg.POST.MaxNumUnits, "")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.K1, "post-k1",
		cfg.POST.K1, "difficulty factor for finding a good label when generating a proof")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.K2, "post-k2",
		cfg.POST.K2, "number of labels to prove")
	cmd.PersistentFlags().Uint32Var(&cfg.POST.K3, "post-k3",
		cfg.POST.K3, "subset of labels to verify in a proof")
	cmd.PersistentFlags().VarP(&cfg.POST.PowDifficulty, "post-pow-difficulty", "", "difficulty of randomx-based proof of work")

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
	cmd.PersistentFlags().VarP(&cfg.SMESHING.Opts.ProviderID, "smeshing-opts-provider",
		"", "")
	cmd.PersistentFlags().BoolVar(&cfg.SMESHING.Opts.Throttle, "smeshing-opts-throttle",
		cfg.SMESHING.Opts.Throttle, "")

	/**======================== PoST Proving Flags ========================== **/

	cmd.PersistentFlags().UintVar(&cfg.SMESHING.ProvingOpts.Threads, "smeshing-opts-proving-threads",
		cfg.SMESHING.ProvingOpts.Threads, "")
	cmd.PersistentFlags().UintVar(&cfg.SMESHING.ProvingOpts.Nonces, "smeshing-opts-proving-nonces",
		cfg.SMESHING.ProvingOpts.Nonces, "")

	/**======================== PoST Verifying Flags ========================== **/

	cmd.PersistentFlags().IntVar(
		&cfg.SMESHING.VerifyingOpts.MinWorkers,
		"smeshing-opts-verifying-min-threads",
		cfg.SMESHING.VerifyingOpts.MinWorkers,
		"Minimal number of threads to use for verifying PoSTs (used while PoST is generated)",
	)
	cmd.PersistentFlags().IntVar(&cfg.SMESHING.VerifyingOpts.Workers, "smeshing-opts-verifying-threads",
		cfg.SMESHING.VerifyingOpts.Workers, "")

	/**======================== Consensus Flags ========================== **/

	cmd.PersistentFlags().Uint32Var(&cfg.LayersPerEpoch, "layers-per-epoch",
		cfg.LayersPerEpoch, "number of layers in epoch")

	/**======================== PoET Flags ========================== **/

	cmd.PersistentFlags().DurationVar(&cfg.POET.PhaseShift, "phase-shift",
		cfg.POET.PhaseShift, "phase shift of poet server")
	cmd.PersistentFlags().DurationVar(&cfg.POET.CycleGap, "cycle-gap",
		cfg.POET.CycleGap, "cycle gap of poet server")
	cmd.PersistentFlags().DurationVar(&cfg.POET.GracePeriod, "grace-period",
		cfg.POET.GracePeriod, "time before PoET round starts when the node builds and submits a challenge")
	cmd.PersistentFlags().DurationVar(&cfg.POET.RequestTimeout, "poet-request-timeout",
		cfg.POET.RequestTimeout, "timeout for poet requests")

	/**======================== bootstrap data updater Flags ========================== **/
	cmd.PersistentFlags().StringVar(&cfg.Bootstrap.URL, "bootstrap-url",
		cfg.Bootstrap.URL, "the url to query bootstrap data update")
	cmd.PersistentFlags().StringVar(&cfg.Bootstrap.Version, "bootstrap-version",
		cfg.Bootstrap.Version, "the update version of the bootstrap data")

	/**======================== testing related flags ========================== **/
	cmd.PersistentFlags().StringVar(&cfg.TestConfig.SmesherKey, "testing-smesher-key",
		"", "import private smesher key for testing",
	)

	// Bind Flags to config
	err := viper.BindPFlags(cmd.PersistentFlags())
	if err != nil {
		fmt.Println("an error has occurred while binding flags:", err)
	}
}
