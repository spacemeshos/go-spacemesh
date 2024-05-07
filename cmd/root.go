package cmd

import (
	"fmt"

	"github.com/spf13/pflag"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/config/presets"
	"github.com/spacemeshos/go-spacemesh/node/flags"
)

func AddFlags(flagSet *pflag.FlagSet, cfg *config.Config) (configPath *string) {
	// A workaround to keep the original config intact to avoid
	// overwriting it with the default values.
	original := *cfg
	defer func() { *cfg = original }()

	configPath = flagSet.StringP("config", "c", "", "load configuration from file")
	flagSet.StringVarP(&cfg.Preset, "preset", "p", "",
		fmt.Sprintf("preset overwrites default values of the config. options %s", presets.Options()))

	/** ======================== Checkpoint Flags ========================== **/
	flagSet.StringVar(&cfg.Recovery.Uri,
		"recovery-uri", cfg.Recovery.Uri, "reset the node state based on the supplied checkpoint file")
	flagSet.Uint32Var(&cfg.Recovery.Restore,
		"recovery-layer", cfg.Recovery.Restore, "restart the mesh with the checkpoint file at this layer")

	/** ======================== BaseConfig Flags ========================== **/
	flagSet.StringVarP(&cfg.BaseConfig.DataDirParent, "data-folder", "d",
		cfg.BaseConfig.DataDirParent, "Specify data directory for spacemesh")
	flagSet.StringVar(&cfg.BaseConfig.FileLock,
		"filelock", cfg.BaseConfig.FileLock, "Filesystem lock to prevent running more than one instance.")
	flagSet.StringVar(&cfg.LOGGING.Encoder, "log-encoder",
		cfg.LOGGING.Encoder, "Log as JSON instead of plain text")
	flagSet.BoolVar(&cfg.CollectMetrics, "metrics",
		cfg.CollectMetrics, "collect node metrics")
	flagSet.IntVar(&cfg.MetricsPort, "metrics-port",
		cfg.MetricsPort, "metric server port")
	flagSet.StringVar(&cfg.PublicMetrics.MetricsURL, "metrics-push",
		cfg.PublicMetrics.MetricsURL, "Push metrics to url")
	flagSet.DurationVar(&cfg.PublicMetrics.MetricsPushPeriod, "metrics-push-period",
		cfg.PublicMetrics.MetricsPushPeriod, "Push period")
	flagSet.Var(
		&flags.JSONFlag{Value: &cfg.PoetServers},
		"poet-servers",
		"JSON-encoded list of poet servers (address and pubkey)",
	)
	flagSet.StringVar(&cfg.Genesis.GenesisTime, "genesis-time",
		cfg.Genesis.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	flagSet.StringVar(&cfg.Genesis.ExtraData, "genesis-extra-data",
		cfg.Genesis.ExtraData, "genesis extra-data will be committed to the genesis id")
	flagSet.DurationVar(&cfg.LayerDuration, "layer-duration",
		cfg.LayerDuration, "Duration between layers")
	flagSet.Uint32Var(&cfg.LayerAvgSize, "layer-average-size",
		cfg.LayerAvgSize, "Layer Avg size")
	flagSet.BoolVar(&cfg.PprofHTTPServer, "pprof-server",
		cfg.PprofHTTPServer, "enable http pprof server")
	flagSet.StringVar(&cfg.PprofHTTPServerListener, "pprof-listener", cfg.PprofHTTPServerListener,
		"Listen address for pprof server, not safe to expose publicly")
	flagSet.Uint64Var(&cfg.TickSize, "tick-size", cfg.TickSize, "number of poet leaves in a single tick")
	flagSet.StringVar(&cfg.ProfilerURL, "profiler-url", cfg.ProfilerURL,
		"send profiler data to certain url, if no url no profiling will be sent, format: http://<IP>:<PORT>")
	flagSet.StringVar(&cfg.ProfilerName, "profiler-name",
		cfg.ProfilerName, "the name to use when sending profiles")

	flagSet.IntVar(&cfg.TxsPerProposal, "txs-per-proposal",
		cfg.TxsPerProposal, "the number of transactions to select per proposal")
	flagSet.Uint64Var(&cfg.BlockGasLimit, "block-gas-limit",
		cfg.BlockGasLimit, "max gas allowed per block")
	flagSet.IntVar(&cfg.OptFilterThreshold, "optimistic-filtering-threshold",
		cfg.OptFilterThreshold, "threshold for optimistic filtering in percentage")

	flagSet.IntVar(&cfg.DatabaseConnections, "db-connections",
		cfg.DatabaseConnections, "configure number of active connections to enable parallel read requests")
	flagSet.BoolVar(&cfg.DatabaseLatencyMetering, "db-latency-metering",
		cfg.DatabaseLatencyMetering, "if enabled collect latency histogram for every database query")
	flagSet.DurationVar(&cfg.DatabasePruneInterval, "db-prune-interval",
		cfg.DatabasePruneInterval, "configure interval for database pruning")

	flagSet.BoolVar(&cfg.ScanMalfeasantATXs, "scan-malfeasant-atxs", cfg.ScanMalfeasantATXs,
		"scan for malfeasant ATXs")

	flagSet.BoolVar(&cfg.NoMainOverride, "no-main-override",
		cfg.NoMainOverride, "force 'nomain' builds to run on the mainnet")

	/** ======================== P2P Flags ========================== **/

	flagSet.Var(flags.NewAddressListValue(cfg.P2P.Listen, &cfg.P2P.Listen),
		"listen", "address(es) for listening")
	flagSet.BoolVar(&cfg.P2P.Flood, "flood",
		cfg.P2P.Flood, "flood created messages to all peers")
	flagSet.BoolVar(&cfg.P2P.DisableNatPort, "disable-natport", cfg.P2P.DisableNatPort,
		"disable nat port-mapping (if enabled upnp protocol is used to negotiate external port with router)")
	flagSet.BoolVar(&cfg.P2P.DisableReusePort,
		"disable-reuseport",
		cfg.P2P.DisableReusePort,
		"disables SO_REUSEPORT for tcp sockets. Try disabling this if your node can't reach bootnodes in the network",
	)
	flagSet.BoolVar(&cfg.P2P.Metrics,
		"p2p-metrics",
		cfg.P2P.Metrics,
		"enable extended metrics collection from libp2p components",
	)
	flagSet.IntVar(&cfg.P2P.AcceptQueue,
		"p2p-accept-queue",
		cfg.P2P.AcceptQueue,
		"number of connections that are fully setup before accepting new connections",
	)
	flagSet.IntVar(&cfg.P2P.LowPeers, "low-peers",
		cfg.P2P.LowPeers, "low watermark for the number of connections")
	flagSet.IntVar(&cfg.P2P.HighPeers, "high-peers",
		cfg.P2P.HighPeers,
		"high watermark for number of connections; once reached, connections are pruned until low watermark remains")
	flagSet.IntVar(&cfg.P2P.MinPeers, "min-peers",
		cfg.P2P.MinPeers, "actively search for peers until you get this much")
	flagSet.StringSliceVar(&cfg.P2P.Bootnodes, "bootnodes",
		cfg.P2P.Bootnodes, "entrypoints into the network")
	flagSet.StringSliceVar(&cfg.P2P.PingPeers, "ping-peers", cfg.P2P.Bootnodes, "peers to ping")
	flagSet.DurationVar(&cfg.P2P.PingInterval, "ping-interval", cfg.P2P.PingInterval, "ping interval")
	flagSet.StringSliceVar(&cfg.P2P.StaticRelays, "static-relays",
		cfg.P2P.StaticRelays, "static relay list")
	flagSet.Var(flags.NewAddressListValue(cfg.P2P.AdvertiseAddress, &cfg.P2P.AdvertiseAddress),
		"advertise-address",
		"libp2p address(es) with identity (example: /dns4/bootnode.spacemesh.io/tcp/5003)")
	flagSet.BoolVar(&cfg.P2P.Bootnode, "p2p-bootnode", cfg.P2P.Bootnode,
		"gossipsub and discovery will be running in a mode suitable for bootnode")
	flagSet.BoolVar(&cfg.P2P.PrivateNetwork, "p2p-private-network", cfg.P2P.PrivateNetwork,
		"discovery will work in private mode. mostly useful for testing, don't set in public networks")
	flagSet.BoolVar(&cfg.P2P.ForceDHTServer, "force-dht-server", cfg.P2P.ForceDHTServer,
		"force DHT server mode")
	flagSet.BoolVar(&cfg.P2P.EnableTCPTransport, "enable-tcp-transport", cfg.P2P.EnableTCPTransport,
		"enable TCP transport")
	flagSet.BoolVar(&cfg.P2P.EnableQUICTransport, "enable-quic-transport", cfg.P2P.EnableQUICTransport,
		"enable QUIC transport")
	flagSet.BoolVar(&cfg.P2P.EnableRoutingDiscovery, "enable-routing-discovery", cfg.P2P.EnableQUICTransport,
		"enable routing discovery")
	flagSet.BoolVar(&cfg.P2P.RoutingDiscoveryAdvertise, "routing-discovery-advertise",
		cfg.P2P.RoutingDiscoveryAdvertise, "advertise for routing discovery")

	/** ======================== TIME Flags ========================== **/

	flagSet.BoolVar(&cfg.TIME.Peersync.Disable, "peersync-disable", cfg.TIME.Peersync.Disable,
		"disable verification that local time is in sync with peers")
	flagSet.DurationVar(&cfg.TIME.Peersync.RoundRetryInterval, "peersync-round-retry-interval",
		cfg.TIME.Peersync.RoundRetryInterval, "when to retry a sync round after a failure")
	flagSet.DurationVar(&cfg.TIME.Peersync.RoundInterval, "peersync-round-interval",
		cfg.TIME.Peersync.RoundRetryInterval, "when to run a next sync round")
	flagSet.DurationVar(&cfg.TIME.Peersync.RoundTimeout, "peersync-round-timeout",
		cfg.TIME.Peersync.RoundRetryInterval, "how long to wait for a round to complete")
	flagSet.DurationVar(&cfg.TIME.Peersync.MaxClockOffset, "peersync-max-clock-offset",
		cfg.TIME.Peersync.MaxClockOffset, "max difference between local clock and peers clock")
	flagSet.IntVar(&cfg.TIME.Peersync.MaxOffsetErrors, "peersync-max-offset-errors", cfg.TIME.Peersync.MaxOffsetErrors,
		"node will exit when max number of consecutive offset errors has been reached")
	flagSet.IntVar(&cfg.TIME.Peersync.RequiredResponses, "peersync-required-responses",
		cfg.TIME.Peersync.RequiredResponses, "min number of clock samples fetched from others to verify time")

	/** ======================== API Flags ========================== **/

	flagSet.StringVar(&cfg.API.PublicListener, "grpc-public-listener",
		cfg.API.PublicListener, "Socket for grpc services that are save to expose publicly.")
	flagSet.StringVar(&cfg.API.PrivateListener, "grpc-private-listener",
		cfg.API.PrivateListener, "Socket for grpc services that are not safe to expose publicly.")
	flagSet.StringVar(&cfg.API.PostListener, "grpc-post-listener", cfg.API.PostListener,
		"Socket on which the node listens for post service connections.")
	flagSet.StringSliceVar(&cfg.API.TLSServices, "grpc-tls-services",
		cfg.API.TLSServices, "List of services that to be exposed via TLS Listener.")
	flagSet.StringVar(&cfg.API.TLSListener, "grpc-tls-listener",
		cfg.API.TLSListener, "Socket for the grpc services using mTLS.")
	flagSet.StringVar(&cfg.API.TLSCACert, "gprc-tls-ca-cert",
		cfg.API.TLSCACert, "Path to the file containing the CA certificate for mTLS.")
	flagSet.StringVar(&cfg.API.TLSCert, "grpc-tls-cert",
		cfg.API.TLSCert, "Path to the file containing the nodes certificate for mTLS.")
	flagSet.StringVar(&cfg.API.TLSKey, "grpc-tls-key",
		cfg.API.TLSKey, "Path to the file containing the nodes private key for mTLS.")
	flagSet.IntVar(&cfg.API.GrpcRecvMsgSize, "grpc-recv-msg-size",
		cfg.API.GrpcRecvMsgSize, "GRPC api recv message size")
	flagSet.IntVar(&cfg.API.GrpcSendMsgSize, "grpc-send-msg-size",
		cfg.API.GrpcSendMsgSize, "GRPC api send message size")
	flagSet.StringVar(&cfg.API.JSONListener, "grpc-json-listener",
		cfg.API.JSONListener, "(Optional) endpoint to expose public grpc services via HTTP/JSON.")

	/**======================== Hare Eligibility Oracle Flags ========================== **/

	flagSet.Uint32Var(&cfg.HareEligibility.ConfidenceParam, "eligibility-confidence-param",
		cfg.HareEligibility.ConfidenceParam,
		"The relative layer (with respect to the current layer) we are confident to have consensus about")

	/**======================== Beacon Flags ========================== **/

	flagSet.IntVar(&cfg.Beacon.Kappa, "beacon-kappa",
		cfg.Beacon.Kappa, "Security parameter (for calculating ATX threshold)")
	flagSet.Var((*types.RatVar)(&cfg.Beacon.Q), "beacon-q",
		"Ratio of dishonest spacetime (for calculating ATX threshold). Should be a string representing a rational.")
	flagSet.Uint32Var((*uint32)(&cfg.Beacon.RoundsNumber), "beacon-rounds-number",
		uint32(cfg.Beacon.RoundsNumber), "Amount of rounds in every epoch")
	flagSet.DurationVar(&cfg.Beacon.GracePeriodDuration, "beacon-grace-period-duration",
		cfg.Beacon.GracePeriodDuration, "Grace period duration in milliseconds")
	flagSet.DurationVar(&cfg.Beacon.ProposalDuration, "beacon-proposal-duration",
		cfg.Beacon.ProposalDuration, "Proposal duration in milliseconds")
	flagSet.DurationVar(&cfg.Beacon.FirstVotingRoundDuration, "beacon-first-voting-round-duration",
		cfg.Beacon.FirstVotingRoundDuration, "First voting round duration in milliseconds")
	flagSet.DurationVar(&cfg.Beacon.VotingRoundDuration, "beacon-voting-round-duration",
		cfg.Beacon.VotingRoundDuration, "Voting round duration in milliseconds")
	flagSet.DurationVar(&cfg.Beacon.WeakCoinRoundDuration, "beacon-weak-coin-round-duration",
		cfg.Beacon.WeakCoinRoundDuration, "Weak coin round duration in milliseconds")
	flagSet.Var((*types.RatVar)(&cfg.Beacon.Theta), "beacon-theta",
		"Ratio of votes for reaching consensus")
	flagSet.Uint32Var(&cfg.Beacon.VotesLimit, "beacon-votes-limit",
		cfg.Beacon.VotesLimit, "Maximum allowed number of votes to be sent")
	flagSet.IntVar(&cfg.Beacon.BeaconSyncWeightUnits, "beacon-sync-weight-units",
		cfg.Beacon.BeaconSyncWeightUnits, "Numbers of weight units to wait before determining beacon values from them.")

	/**======================== Tortoise Flags ========================== **/
	flagSet.Uint32Var(&cfg.Tortoise.Hdist, "tortoise-hdist",
		cfg.Tortoise.Hdist, "the distance for tortoise to vote according to hare output")
	flagSet.Uint32Var(&cfg.Tortoise.Zdist, "tortoise-zdist",
		cfg.Tortoise.Zdist, "the distance for tortoise to wait for hare output")
	flagSet.Uint32Var(&cfg.Tortoise.WindowSize, "tortoise-window-size",
		cfg.Tortoise.WindowSize, "size of the tortoise sliding window in layers")
	flagSet.IntVar(&cfg.Tortoise.MaxExceptions, "tortoise-max-exceptions",
		cfg.Tortoise.MaxExceptions, "number of exceptions tolerated for a base ballot")
	flagSet.Uint32Var(&cfg.Tortoise.BadBeaconVoteDelayLayers, "tortoise-delay-layers",
		cfg.Tortoise.BadBeaconVoteDelayLayers, "number of layers to ignore a ballot with a different beacon")
	flagSet.BoolVar(&cfg.Tortoise.EnableTracer, "tortoise-enable-tracer",
		cfg.Tortoise.EnableTracer, "record every tortoise input/output to the logging output")

	// TODO(moshababo): add usage desc
	flagSet.Uint64Var(&cfg.POST.LabelsPerUnit, "post-labels-per-unit",
		cfg.POST.LabelsPerUnit, "")
	flagSet.Uint32Var(&cfg.POST.MinNumUnits, "post-min-numunits",
		cfg.POST.MinNumUnits, "")
	flagSet.Uint32Var(&cfg.POST.MaxNumUnits, "post-max-numunits",
		cfg.POST.MaxNumUnits, "")
	flagSet.UintVar(&cfg.POST.K1, "post-k1",
		cfg.POST.K1, "difficulty factor for finding a good label when generating a proof")
	flagSet.UintVar(&cfg.POST.K2, "post-k2",
		cfg.POST.K2, "number of labels to prove")
	flagSet.UintVar(
		&cfg.POST.K3,
		"post-k3",
		cfg.POST.K3,
		"size of the subset of labels to verify in POST proofs\n"+
			"lower values will result in faster ATX verification but increase the risk\n"+
			"as the node must depend on malfeasance proofs to detect invalid ATXs",
	)
	flagSet.AddFlag(&pflag.Flag{
		Name:     "post-pow-difficulty",
		Value:    &cfg.POST.PowDifficulty,
		DefValue: cfg.POST.PowDifficulty.String(),
		Usage:    "difficulty of randomx-based proof of work",
	})

	/**======================== Smeshing Flags ========================== **/

	// TODO(moshababo): add usage desc

	flagSet.BoolVar(&cfg.SMESHING.Start, "smeshing-start",
		cfg.SMESHING.Start, "")
	flagSet.StringVar(&cfg.SMESHING.CoinbaseAccount, "smeshing-coinbase",
		cfg.SMESHING.CoinbaseAccount, "coinbase account to accumulate rewards")
	flagSet.StringVar(&cfg.SMESHING.Opts.DataDir, "smeshing-opts-datadir",
		cfg.SMESHING.Opts.DataDir, "")
	flagSet.Uint32Var(&cfg.SMESHING.Opts.NumUnits, "smeshing-opts-numunits",
		cfg.SMESHING.Opts.NumUnits, "")
	flagSet.Uint64Var(&cfg.SMESHING.Opts.MaxFileSize, "smeshing-opts-maxfilesize",
		cfg.SMESHING.Opts.MaxFileSize, "")
	flagSet.AddFlag(&pflag.Flag{
		Name:     "smeshing-opts-provider",
		Value:    &cfg.SMESHING.Opts.ProviderID,
		DefValue: cfg.SMESHING.Opts.ProviderID.String(),
	})
	flagSet.BoolVar(&cfg.SMESHING.Opts.Throttle, "smeshing-opts-throttle",
		cfg.SMESHING.Opts.Throttle, "")

	/**======================== PoST Proving Flags ========================== **/

	flagSet.UintVar(&cfg.SMESHING.ProvingOpts.Threads, "smeshing-opts-proving-threads",
		cfg.SMESHING.ProvingOpts.Threads, "")
	flagSet.UintVar(&cfg.SMESHING.ProvingOpts.Nonces, "smeshing-opts-proving-nonces",
		cfg.SMESHING.ProvingOpts.Nonces, "")
	flagSet.AddFlag(&pflag.Flag{
		Name:     "smeshing-opts-proving-randomx-mode",
		Value:    &cfg.SMESHING.ProvingOpts.RandomXMode,
		DefValue: cfg.SMESHING.ProvingOpts.RandomXMode.String(),
	})

	/**======================== PoST Verifying Flags ========================== **/

	flagSet.BoolVar(
		&cfg.SMESHING.VerifyingOpts.Disabled,
		"smeshing-opts-verifying-disable",
		false,
		"Disable verifying POST proofs. Experimental.\n"+
			"Use with caution, only on private nodes with a trusted public peer that validates the proofs.",
	)
	flagSet.IntVar(
		&cfg.SMESHING.VerifyingOpts.MinWorkers,
		"smeshing-opts-verifying-min-workers",
		cfg.SMESHING.VerifyingOpts.MinWorkers,
		"Minimal number of threads to use for verifying PoSTs (used while PoST is generated)",
	)
	flagSet.IntVar(&cfg.SMESHING.VerifyingOpts.Workers, "smeshing-opts-verifying-workers",
		cfg.SMESHING.VerifyingOpts.Workers, "")
	flagSet.AddFlag(&pflag.Flag{
		Name:     "smeshing-opts-verifying-powflags",
		Value:    &cfg.SMESHING.VerifyingOpts.Flags,
		DefValue: cfg.SMESHING.VerifyingOpts.Flags.String(),
	})

	/**======================== Consensus Flags ========================== **/

	flagSet.Uint32Var(&cfg.LayersPerEpoch, "layers-per-epoch",
		cfg.LayersPerEpoch, "number of layers in epoch")

	/**======================== PoET Flags ========================== **/

	flagSet.DurationVar(&cfg.POET.PhaseShift, "phase-shift",
		cfg.POET.PhaseShift, "phase shift of poet server")
	flagSet.DurationVar(&cfg.POET.CycleGap, "cycle-gap",
		cfg.POET.CycleGap, "cycle gap of poet server")
	flagSet.DurationVar(&cfg.POET.GracePeriod, "grace-period",
		cfg.POET.GracePeriod, "time before PoET round starts when the node builds and submits a challenge")
	flagSet.DurationVar(&cfg.POET.RequestTimeout, "poet-request-timeout",
		cfg.POET.RequestTimeout, "timeout for poet requests")

	/**======================== bootstrap data updater Flags ========================== **/

	flagSet.StringVar(&cfg.Bootstrap.URL, "bootstrap-url",
		cfg.Bootstrap.URL, "the url to query bootstrap data update")
	flagSet.StringVar(&cfg.Bootstrap.Version, "bootstrap-version",
		cfg.Bootstrap.Version, "the update version of the bootstrap data")

	/**======================== testing related flags ========================== **/
	flagSet.StringVar(&cfg.TestConfig.SmesherKey, "testing-smesher-key",
		"", "import private smesher key for testing",
	)
	flagSet.VarP(flags.NewStringToUint64Value(&cfg.Genesis.Accounts), "accounts", "a",
		"List of pre-funded accounts (use in tests only")

	/**========================  Deprecated flags ========================== **/
	flagSet.Var(flags.NewDeprecatedFlag(
		config.DeprecatedPoETServers{}), "poet-server", "deprecated, use poet-servers instead")
	if err := flagSet.MarkHidden("poet-server"); err != nil {
		panic(err) // unreachable
	}

	return configPath
}
