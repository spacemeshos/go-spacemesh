package cmd

import (
	"fmt"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
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
	cmd.PersistentFlags().StringVar(&config.OracleServer, "oracle_server",
		config.OracleServer, "The oracle server url. (temporary) ")
	cmd.PersistentFlags().IntVar(&config.OracleServerWorldID, "oracle_server_worldid",
		config.OracleServerWorldID, "The worldid to use with the oracle server (temporary) ")
	cmd.PersistentFlags().StringVar(&config.PoETServer, "poet-server",
		config.OracleServer, "The poet server url. (temporary) ")
	cmd.PersistentFlags().StringVar(&config.GenesisTime, "genesis-time",
		config.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().IntVar(&config.LayerDurationSec, "layer-duration-sec",
		config.LayerDurationSec, "Duration between layers in seconds")
	cmd.PersistentFlags().IntVar(&config.LayerAvgSize, "layer-average-size",
		config.LayerAvgSize, "Layer Avg size")
	cmd.PersistentFlags().IntVar(&config.Hdist, "hdist",
		config.Hdist, "hdist")
	cmd.PersistentFlags().BoolVar(&config.StartMining, "start-mining",
		config.StartMining, "start mining")
	cmd.PersistentFlags().StringVar(&config.MemProfile, "mem-profile",
		config.MemProfile, "output memory profiling stat to filename")
	cmd.PersistentFlags().StringVar(&config.CPUProfile, "cpu-profile",
		config.CPUProfile, "output cpu profiling stat to filename")
	cmd.PersistentFlags().BoolVar(&config.PprofHTTPServer, "pprof-server",
		config.PprofHTTPServer, "enable http pprof server")
	cmd.PersistentFlags().StringVar(&config.GenesisConfPath, "genesis-conf",
		config.GenesisConfPath, "add genesis configuration")
	cmd.PersistentFlags().StringVar(&config.CoinbaseAccount, "coinbase",
		config.CoinbaseAccount, "coinbase account to accumulate rewards")
	cmd.PersistentFlags().IntVar(&config.GenesisActiveSet, "genesis-active-size",
		config.GenesisActiveSet, "The active set size for the genesis flow")
	cmd.PersistentFlags().IntVar(&config.BlockCacheSize, "block-cache-size",
		config.BlockCacheSize, "size in layers of meshdb block cache")
	cmd.PersistentFlags().StringVar(&config.PublishEventsURL, "events-url",
		config.PublishEventsURL, "publish events to this url; if no url specified no events will be published")

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
	cmd.PersistentFlags().Int8Var(&config.P2P.NetworkID, "network-id",
		config.P2P.NetworkID, "NetworkID to run on (0 - mainnet, 1 - testnet)")
	cmd.PersistentFlags().DurationVar(&config.P2P.ResponseTimeout, "response-timeout",
		config.P2P.ResponseTimeout, "Timeout for waiting on resposne message")
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
	cmd.PersistentFlags().IntVar(&config.P2P.MsgSizeLimit, "msg-size-limit",
		config.P2P.MsgSizeLimit, "The message size limit in bytes for incoming messages")

	/** ======================== API Flags ========================== **/

	// StartJSONApiServerFlag determines if json api server should be started
	cmd.PersistentFlags().BoolVar(&config.API.StartJSONServer, "json-server",
		config.API.StartJSONServer, "StartService the json http server. "+
			"Note that starting the Json server also starts the grpc server. ",
	)
	// StartJSONApiServerFlag determines if json api server should be started
	cmd.PersistentFlags().BoolVar(&config.API.StartNewJSONServer, "json-server-new",
		config.API.StartNewJSONServer, "Start the new grpc-gateway (json http) server. "+
			"The gateway server will be enabled for all corresponding, enabled GRPC services.",
	)
	// JSONServerPortFlag determines the json api server local listening port
	cmd.PersistentFlags().IntVar(&config.API.JSONServerPort, "json-port",
		config.API.JSONServerPort, "JSON api server port")
	// NewJSONServerPortFlag determines the json api server local listening port (for new server)
	cmd.PersistentFlags().IntVar(&config.API.NewJSONServerPort, "json-port-new",
		config.API.NewJSONServerPort, "New JSON api server port")
	// StartGrpcAPIServerFlag determines if the grpc server should be started
	cmd.PersistentFlags().BoolVar(&config.API.StartGrpcServer, "grpc-server",
		config.API.StartGrpcServer, "StartService the grpc server. "+
			"Note: This service will soon be deprecated. Use --grpc instead.")
	cmd.PersistentFlags().StringSliceVar(&config.API.StartGrpcServices, "grpc",
		config.API.StartGrpcServices, "Comma-separated list of individual grpc services to enable "+
			"(node,mesh,globalstate,transaction,smesher)")
	// GrpcServerPortFlag determines the grpc server local listening port
	cmd.PersistentFlags().IntVar(&config.API.GrpcServerPort, "grpc-port",
		config.API.GrpcServerPort, "GRPC api server port")
	// NewGrpcServerFlag determines the grpc server local listening port (for new server)
	cmd.PersistentFlags().IntVar(&config.API.NewGrpcServerPort, "grpc-port-new",
		config.API.NewGrpcServerPort, "New GRPC api server port")
	// NewGrpcServerInterface determines the interface the GRPC server listens on
	cmd.PersistentFlags().StringVar(&config.API.NewGrpcServerInterface, "grpc-interface-new",
		config.API.NewGrpcServerInterface, "New GRPC api server interface")

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

	cmd.PersistentFlags().Uint64Var(&config.HareEligibility.ConfidenceParam, "eligibility-confidence-param",
		config.HareEligibility.ConfidenceParam, "The relative layer (with respect to the current layer) we are confident to have consensus about")
	cmd.PersistentFlags().IntVar(&config.HareEligibility.EpochOffset, "eligibility-epoch-offset",
		config.HareEligibility.EpochOffset, "The constant layer (within an epoch) for which we traverse its view for the purpose of counting consensus active set")

	/**======================== PoST Flags ========================== **/

	cmd.PersistentFlags().StringVar(&config.POST.DataDir, "post-datadir",
		config.POST.DataDir, "The directory that contains post data files")
	cmd.PersistentFlags().Uint64Var(&config.POST.SpacePerUnit, "post-space",
		config.POST.SpacePerUnit, "Space per unit, in bytes")
	cmd.PersistentFlags().IntVar(&config.POST.NumFiles, "post-numfiles",
		config.POST.NumFiles, "Number of files")
	cmd.PersistentFlags().UintVar(&config.POST.Difficulty, "post-difficulty",
		config.POST.Difficulty, "Computational cost of the initialization")
	cmd.PersistentFlags().UintVar(&config.POST.NumProvenLabels, "post-labels",
		config.POST.NumProvenLabels, "Number of labels to prove in non-interactive proof (security parameter)")
	cmd.PersistentFlags().UintVar(&config.POST.LowestLayerToCacheDuringProofGeneration, "post-cachelayer",
		config.POST.LowestLayerToCacheDuringProofGeneration, "Lowest layer to cache in-memory during proof generation (optimization parameter)")
	cmd.PersistentFlags().Uint64Var(&config.POST.LabelsLogRate, "post-lograte",
		config.POST.LabelsLogRate, "Labels construction progress log rate")
	cmd.PersistentFlags().UintVar(&config.POST.MaxWriteFilesParallelism, "post-parallel-files",
		config.POST.MaxWriteFilesParallelism, "Max degree of files write parallelism")
	cmd.PersistentFlags().UintVar(&config.POST.MaxWriteInFileParallelism, "post-parallel-infile",
		config.POST.MaxWriteInFileParallelism, "Max degree of cpu work parallelism per file write")
	cmd.PersistentFlags().UintVar(&config.POST.MaxReadFilesParallelism, "post-parallel-read",
		config.POST.MaxReadFilesParallelism, "Max degree of files read parallelism")

	/**========================Consensus Flags ========================== **/

	cmd.PersistentFlags().IntVar(&config.LayersPerEpoch, "layers-per-epoch",
		config.LayersPerEpoch, "number of layers in epoch")

	// Bind Flags to config
	err := viper.BindPFlags(cmd.PersistentFlags())
	if err != nil {
		fmt.Println("an error has occurred while binding flags:", err)
	}

}
