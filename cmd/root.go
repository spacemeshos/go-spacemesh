package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	cfg "github.com/spacemeshos/go-spacemesh/config"
)

var (
	config = cfg.DefaultConfig()
)

// AddCommands adds cobra commands to the app.
func AddCommands(cmd *cobra.Command) {

	/** ======================== BaseConfig Flags ========================== **/

	cmd.PersistentFlags().StringVarP(&config.BaseConfig.ConfigFile,
		"config", "c", config.BaseConfig.ConfigFile, "Set Load configuration from file")
	cmd.PersistentFlags().StringVarP(&config.BaseConfig.DataDirParent, "data-folder", "d",
		config.BaseConfig.DataDirParent, "Specify data directory for spacemesh")
	cmd.PersistentFlags().BoolVar(&config.TestMode, "test-mode",
		config.TestMode, "Initialize testing features")
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
	cmd.PersistentFlags().StringVar(&config.PoETServer, "poet-server",
		config.PoETServer, "The poet server url. (temporary) ")
	cmd.PersistentFlags().StringVar(&config.GenesisTime, "genesis-time",
		config.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().IntVar(&config.LayerDurationSec, "layer-duration-sec",
		config.LayerDurationSec, "Duration between layers in seconds")
	cmd.PersistentFlags().IntVar(&config.LayerAvgSize, "layer-average-size",
		config.LayerAvgSize, "Layer Avg size")
	cmd.PersistentFlags().Uint32Var(&config.Hdist, "hdist",
		config.Hdist, "hdist")
	cmd.PersistentFlags().BoolVar(&config.PprofHTTPServer, "pprof-server",
		config.PprofHTTPServer, "enable http pprof server")
	cmd.PersistentFlags().StringVar(&config.GoldenATXID, "golden-atx",
		config.GoldenATXID, "golden ATX hash")
	cmd.PersistentFlags().IntVar(&config.BlockCacheSize, "block-cache-size",
		config.BlockCacheSize, "size in layers of meshdb block cache")
	cmd.PersistentFlags().StringVar(&config.PublishEventsURL, "events-url",
		config.PublishEventsURL, "publish events to this url; if no url specified no events will be published")
	cmd.PersistentFlags().StringVar(&config.ProfilerURL, "profiler-url",
		config.ProfilerURL, "send profiler data to certain url, if no url no profiling will be sent, format: http://<IP>:<PORT>")
	cmd.PersistentFlags().StringVar(&config.ProfilerName, "profiler-name",
		config.ProfilerURL, "the name to use when sending profiles")

	cmd.PersistentFlags().IntVar(&config.SyncRequestTimeout, "sync-request-timeout",
		config.SyncRequestTimeout, "the timeout in ms for direct requests in the sync")
	cmd.PersistentFlags().IntVar(&config.AtxsPerBlock, "atxs-per-block",
		config.AtxsPerBlock, "the number of atxs to select per block on block creation")
	cmd.PersistentFlags().IntVar(&config.TxsPerBlock, "txs-per-block",
		config.TxsPerBlock, "the number of transactions to select per block on block creation")

	/** ======================== P2P Flags ========================== **/

	cmd.PersistentFlags().IntVar(&config.P2P.TCPPort, "tcp-port",
		config.P2P.TCPPort, "inet port for P2P listener")
	cmd.PersistentFlags().StringVar(&config.P2P.TCPInterface, "tcp-interface",
		config.P2P.TCPInterface, "inet interface for P2P listener, specify as IP address")
	cmd.PersistentFlags().BoolVar(&config.P2P.AcquirePort, "acquire-port",
		config.P2P.AcquirePort, "Should the node attempt to forward the port to this machine on a NAT?")
	cmd.PersistentFlags().DurationVar(&config.P2P.DialTimeout, "dial-timeout",
		config.P2P.DialTimeout, "Network dial timeout duration")
	cmd.PersistentFlags().DurationVar(&config.P2P.ConnKeepAlive, "conn-keepalive",
		config.P2P.ConnKeepAlive, "Network connection keep alive")
	cmd.PersistentFlags().Uint32Var(&config.P2P.NetworkID, "network-id",
		config.P2P.NetworkID, "NetworkID to run on (0 - mainnet, 1 - testnet)")
	cmd.PersistentFlags().DurationVar(&config.P2P.ResponseTimeout, "response-timeout",
		config.P2P.ResponseTimeout, "Timeout for waiting on response message")
	cmd.PersistentFlags().DurationVar(&config.P2P.SessionTimeout, "session-timeout",
		config.P2P.SessionTimeout, "Timeout for waiting on session message")
	cmd.PersistentFlags().StringVar(&config.P2P.NodeID, "node-id",
		config.P2P.NodeID, "Load node data by id (pub key) from local store")
	cmd.PersistentFlags().IntVar(&config.P2P.BufferSize, "buffer-size",
		config.P2P.BufferSize, "Size of the messages handler's buffer")
	cmd.PersistentFlags().IntVar(&config.P2P.MaxPendingConnections, "max-pending-connections",
		config.P2P.MaxPendingConnections, "The maximum number of pending connections")
	cmd.PersistentFlags().IntVar(&config.P2P.OutboundPeersTarget, "outbound-target",
		config.P2P.OutboundPeersTarget, "The outbound peer target we're trying to connect")
	cmd.PersistentFlags().IntVar(&config.P2P.MaxInboundPeers, "max-inbound",
		config.P2P.MaxInboundPeers, "The maximum number of inbound peers ")
	cmd.PersistentFlags().BoolVar(&config.P2P.SwarmConfig.Gossip, "gossip",
		config.P2P.SwarmConfig.Gossip, "should we start a gossiping node?")
	cmd.PersistentFlags().BoolVar(&config.P2P.SwarmConfig.Bootstrap, "bootstrap",
		config.P2P.SwarmConfig.Bootstrap, "Bootstrap the swarm")
	cmd.PersistentFlags().IntVar(&config.P2P.SwarmConfig.RoutingTableBucketSize, "bucketsize",
		config.P2P.SwarmConfig.RoutingTableBucketSize, "The rounding table bucket size")
	cmd.PersistentFlags().IntVar(&config.P2P.SwarmConfig.RoutingTableAlpha, "alpha",
		config.P2P.SwarmConfig.RoutingTableAlpha, "The rounding table Alpha")
	cmd.PersistentFlags().IntVar(&config.P2P.SwarmConfig.RandomConnections, "randcon",
		config.P2P.SwarmConfig.RoutingTableAlpha, "Number of random connections")
	cmd.PersistentFlags().StringSliceVar(&config.P2P.SwarmConfig.BootstrapNodes, "bootnodes",
		config.P2P.SwarmConfig.BootstrapNodes, "Number of random connections")
	cmd.PersistentFlags().DurationVar(&config.TIME.MaxAllowedDrift, "max-allowed-time-drift",
		config.TIME.MaxAllowedDrift, "When to close the app until user resolves time sync problems")
	cmd.PersistentFlags().StringVar(&config.P2P.SwarmConfig.PeersFile, "peers-file",
		config.P2P.SwarmConfig.PeersFile, "addrbook peers file. located under data-dir/<publickey>/<peer-file> not loaded or saved if empty string is given.")
	cmd.PersistentFlags().IntVar(&config.TIME.NtpQueries, "ntp-queries",
		config.TIME.NtpQueries, "Number of ntp queries to do")
	cmd.PersistentFlags().DurationVar(&config.TIME.DefaultTimeoutLatency, "default-timeout-latency",
		config.TIME.DefaultTimeoutLatency, "Default timeout to ntp query")
	cmd.PersistentFlags().DurationVar(&config.TIME.RefreshNtpInterval, "refresh-ntp-interval",
		config.TIME.RefreshNtpInterval, "Refresh intervals to ntp")
	cmd.PersistentFlags().StringSliceVar(&config.TIME.NTPServers,
		"ntp-servers", config.TIME.NTPServers, "A list of NTP servers to query (e.g., 'time.google.com'). Overrides the list in config. Must contain more servers than the number of ntp-queries.")
	cmd.PersistentFlags().IntVar(&config.P2P.MsgSizeLimit, "msg-size-limit",
		config.P2P.MsgSizeLimit, "The message size limit in bytes for incoming messages")

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

	/**======================== Tortoise Beacon Flags ========================== **/

	cmd.PersistentFlags().Uint64Var(&config.TortoiseBeacon.Kappa, "tortoise-beacon-kappa",
		config.TortoiseBeacon.Kappa, "Security parameter (for calculating ATX threshold)")
	cmd.PersistentFlags().StringVar(&config.TortoiseBeacon.Q, "tortoise-beacon-q",
		config.TortoiseBeacon.Q, "Ratio of dishonest spacetime (for calculating ATX threshold). It should be a string representing a rational number.")
	cmd.PersistentFlags().Uint64Var(&config.TortoiseBeacon.RoundsNumber, "tortoise-beacon-rounds-number",
		config.TortoiseBeacon.RoundsNumber, "Amount of rounds in every epoch")
	cmd.PersistentFlags().IntVar(&config.TortoiseBeacon.GracePeriodDurationMs, "tortoise-beacon-grace-period-duration-ms",
		config.TortoiseBeacon.GracePeriodDurationMs, "Grace period duration in milliseconds")
	cmd.PersistentFlags().IntVar(&config.TortoiseBeacon.ProposalDurationMs, "tortoise-beacon-proposal-duration-ms",
		config.TortoiseBeacon.ProposalDurationMs, "Proposal duration in milliseconds")
	cmd.PersistentFlags().IntVar(&config.TortoiseBeacon.FirstVotingRoundDurationMs, "tortoise-beacon-first-voting-round-duration-ms",
		config.TortoiseBeacon.FirstVotingRoundDurationMs, "First voting round duration in milliseconds")
	cmd.PersistentFlags().IntVar(&config.TortoiseBeacon.VotingRoundDurationMs, "tortoise-beacon-voting-round-duration-ms",
		config.TortoiseBeacon.VotingRoundDurationMs, "Voting round duration in milliseconds")
	cmd.PersistentFlags().IntVar(&config.TortoiseBeacon.WeakCoinRoundDurationMs, "tortoise-beacon-weak-coin-round-duration-ms",
		config.TortoiseBeacon.WeakCoinRoundDurationMs, "Weak coin round duration in milliseconds")
	cmd.PersistentFlags().IntVar(&config.TortoiseBeacon.WaitAfterEpochStart, "tortoise-beacon-wait-after-epoch-start-ms",
		config.TortoiseBeacon.WaitAfterEpochStart, "How many milliseconds to wait after a new epoch is started.")
	cmd.PersistentFlags().Float64Var(&config.TortoiseBeacon.Theta, "tortoise-beacon-theta",
		config.TortoiseBeacon.Theta, "Ratio of votes for reaching consensus")
	cmd.PersistentFlags().IntVar(&config.TortoiseBeacon.VotesLimit, "tortoise-beacon-votes-limit",
		config.TortoiseBeacon.VotesLimit, "Maximum allowed number of votes to be sent")

	/**======================== Post Flags ========================== **/

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

	/**========================Consensus Flags ========================== **/

	cmd.PersistentFlags().Uint32Var(&config.LayersPerEpoch, "layers-per-epoch",
		config.LayersPerEpoch, "number of layers in epoch")

	// Bind Flags to config
	err := viper.BindPFlags(cmd.PersistentFlags())
	if err != nil {
		fmt.Println("an error has occurred while binding flags:", err)
	}

}
