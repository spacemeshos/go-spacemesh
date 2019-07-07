package cmd

import (
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	config = cfg.DefaultConfig()
)

func AddCommands(cmd *cobra.Command) {

	/** ======================== BaseConfig Flags ========================== **/
	cmd.PersistentFlags().StringVarP(&config.BaseConfig.ConfigFile,
		"config", "c", config.BaseConfig.ConfigFile, "Set Load configuration from file")
	cmd.PersistentFlags().StringVarP(&config.BaseConfig.DataDir, "data-folder", "d",
		config.BaseConfig.DataDir, "Specify data directory for spacemesh")
	cmd.PersistentFlags().BoolVar(&config.TestMode, "test-mode",
		config.TestMode, "Initialize testing features")
	cmd.PersistentFlags().BoolVar(&config.CollectMetrics, "metrics",
		config.CollectMetrics, "collect node metrics")
	cmd.PersistentFlags().IntVar(&config.MetricsPort, "metrics-port",
		config.MetricsPort, "metric server port")
	cmd.PersistentFlags().StringVar(&config.OracleServer, "oracle_server",
		config.OracleServer, "The oracle server url. (temporary) ")
	cmd.PersistentFlags().IntVar(&config.OracleServerWorldId, "oracle_server_worldid",
		config.OracleServerWorldId, "The worldid to use with the oracle server (temporary) ")
	cmd.PersistentFlags().StringVar(&config.PoETServer, "poet-server",
		config.OracleServer, "The poet server url. (temporary) ")
	cmd.PersistentFlags().StringVar(&config.GenesisTime, "genesis-time",
		config.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().IntVar(&config.LayerDurationSec, "layer-duration-sec",
		config.LayerDurationSec, "Duration between layers in seconds")
	cmd.PersistentFlags().IntVar(&config.LayerAvgSize, "layer-average-size",
		config.LayerDurationSec, "Duration between layers in seconds")
	cmd.PersistentFlags().StringVar(&config.MemProfile, "mem-profile",
		config.MemProfile, "output memory profiling stat to filename")
	cmd.PersistentFlags().StringVar(&config.CpuProfile, "cpu-profile",
		config.CpuProfile, "output cpu profiling stat to filename")
	cmd.PersistentFlags().BoolVar(&config.PprofHttpServer, "pprof-server",
		config.PprofHttpServer, "enable http pprof server")
	cmd.PersistentFlags().StringVar(&config.GenesisConfPath, "genesis-conf",
		config.GenesisConfPath, "add genesis configuration")
	cmd.PersistentFlags().StringVar(&config.CoinbaseAccount, "coinbase",
		config.CoinbaseAccount, "coinbase account to accumulate rewards")
	/** ======================== P2P Flags ========================== **/
	cmd.PersistentFlags().IntVar(&config.P2P.TCPPort, "tcp-port",
		config.P2P.TCPPort, "TCP Port to listen on")
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
	cmd.PersistentFlags().BoolVar(&config.P2P.NewNode, "new-node",
		config.P2P.NewNode, "Load node data by id (pub key) from local store")
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

	/** ======================== API Flags ========================== **/
	// StartJSONApiServerFlag determines if json api server should be started
	cmd.PersistentFlags().BoolVar(&config.API.StartJSONServer, "json-server",
		config.API.StartJSONServer, "StartService the json http server. "+
			"Note that starting the Json server also starts the grpc server.",
	)
	// JSONServerPortFlag determines the json api server local listening port
	cmd.PersistentFlags().IntVar(&config.API.JSONServerPort, "json-port",
		config.API.JSONServerPort, "JSON api server port")
	// StartGrpcAPIServerFlag determines if the grpc server should be started
	cmd.PersistentFlags().BoolVar(&config.API.StartGrpcServer, "grpc-server",
		config.API.StartGrpcServer, "StartService the grpc server")
	// GrpcServerPortFlag determines the grpc server local listening port
	cmd.PersistentFlags().IntVar(&config.API.GrpcServerPort, "grpc-port",
		config.API.GrpcServerPort, "GRPC api server port")

	/**========================Hare Flags ========================== **/

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

	/**========================Consensus Flags ========================== **/

	cmd.PersistentFlags().IntVar(&config.CONSENSUS.LayersPerEpoch, "layers-per-epoch",
		config.CONSENSUS.LayersPerEpoch, "Duration between layers in seconds")

	//todo: add this here

	// Bind Flags to config
	viper.BindPFlags(cmd.PersistentFlags())

}
