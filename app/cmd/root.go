package cmd

import (
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	config = cfg.DefaultConfig()
)

// RootCmd is the application root command
var RootCmd = &cobra.Command{
	Use:   "spacemesh",
	Short: "Spacemesh Core ( PoST )",
}

func init() {

	/** ======================== BaseConfig Flags ========================== **/
	RootCmd.PersistentFlags().StringVarP(&config.BaseConfig.ConfigFile,
		"config", "c", config.BaseConfig.ConfigFile, "Set Load configuration from file")
	RootCmd.PersistentFlags().StringVarP(&config.BaseConfig.DataDir, "data-folder", "d",
		config.BaseConfig.DataDir, "Specify data directory for spacemesh")
	/** ======================== P2P Flags ========================== **/
	RootCmd.PersistentFlags().IntVar(&config.P2P.SecurityParam, "security-param",
		config.P2P.SecurityParam, "Consensus protocol k security param")
	RootCmd.PersistentFlags().IntVar(&config.P2P.TCPPort, "tcp-port",
		config.P2P.TCPPort, "TCP Port to listen on")
	RootCmd.PersistentFlags().DurationVar(&config.P2P.DialTimeout, "dial-timeout",
		config.P2P.DialTimeout, "Network dial timeout duration")
	RootCmd.PersistentFlags().DurationVar(&config.P2P.ConnKeepAlive, "conn-keepalive",
		config.P2P.ConnKeepAlive, "Network connection keep alive")
	RootCmd.PersistentFlags().Int8Var(&config.P2P.NetworkID, "network-id",
		config.P2P.NetworkID, "NetworkID to run on (0 - mainnet, 1 - testnet)")
	RootCmd.PersistentFlags().DurationVar(&config.P2P.ResponseTimeout, "response-timeout",
		config.P2P.ResponseTimeout, "Timeout for waiting on resposne message")
	RootCmd.PersistentFlags().StringVar(&config.P2P.NodeID, "node-id",
		config.P2P.NodeID, "Load node data by id (pub key) from local store")
	RootCmd.PersistentFlags().BoolVar(&config.P2P.NewNode, "new-node",
		config.P2P.NewNode, "Load node data by id (pub key) from local store")
    RootCmd.PersistentFlags().IntVar(&config.P2P.BufferSize, "buffer-size",
		config.P2P.BufferSize, "Size of the messages handler's buffer")
	RootCmd.PersistentFlags().BoolVar(&config.P2P.SwarmConfig.Gossip, "gossip",
		config.P2P.SwarmConfig.Gossip, "should we start a gossiping node?")
	RootCmd.PersistentFlags().BoolVar(&config.P2P.SwarmConfig.Bootstrap, "bootstrap",
		config.P2P.SwarmConfig.Bootstrap, "Bootstrap the swarm")
	RootCmd.PersistentFlags().IntVar(&config.P2P.SwarmConfig.RoutingTableBucketSize, "bucketsize",
		config.P2P.SwarmConfig.RoutingTableBucketSize, "The rounding table bucket size")
	RootCmd.PersistentFlags().IntVar(&config.P2P.SwarmConfig.RoutingTableAlpha, "alpha",
		config.P2P.SwarmConfig.RoutingTableAlpha, "The rounding table Alpha")
	RootCmd.PersistentFlags().IntVar(&config.P2P.SwarmConfig.RandomConnections, "randcon",
		config.P2P.SwarmConfig.RoutingTableAlpha, "Number of random connections")
	RootCmd.PersistentFlags().StringSliceVar(&config.P2P.SwarmConfig.BootstrapNodes, "bootnodes",
		config.P2P.SwarmConfig.BootstrapNodes, "Number of random connections")
	RootCmd.PersistentFlags().DurationVar(&config.P2P.TimeConfig.MaxAllowedDrift, "max-allowed-time-drift",
		config.P2P.TimeConfig.MaxAllowedDrift, "When to close the app until user resolves time sync problems")
	RootCmd.PersistentFlags().IntVar(&config.P2P.TimeConfig.NtpQueries, "ntp-queries",
		config.P2P.TimeConfig.NtpQueries, "Number of ntp queries to do")
	RootCmd.PersistentFlags().DurationVar(&config.P2P.TimeConfig.DefaultTimeoutLatency, "default-timeout-latency",
		config.P2P.TimeConfig.DefaultTimeoutLatency, "Default timeout to ntp query")
	RootCmd.PersistentFlags().DurationVar(&config.P2P.TimeConfig.RefreshNtpInterval, "refresh-ntp-interval",
		config.P2P.TimeConfig.RefreshNtpInterval, "Refresh intervals to ntp")

	/** ======================== API Flags ========================== **/
	// StartJSONApiServerFlag determines if json api server should be started
	RootCmd.PersistentFlags().BoolVar(&config.API.StartJSONServer, "json-server",
		config.API.StartJSONServer, "StartService the json http server. "+
			"Note that starting the Json server also starts the grpc server.",
	)
	// JSONServerPortFlag determines the json api server local listening port
	RootCmd.PersistentFlags().IntVar(&config.API.JSONServerPort, "json-port",
		config.API.JSONServerPort, "JSON api server port")
	// StartGrpcAPIServerFlag determines if the grpc server should be started
	RootCmd.PersistentFlags().BoolVar(&config.API.StartGrpcServer, "grpc-server",
		config.API.StartGrpcServer, "StartService the grpc server")
	// GrpcServerPortFlag determines the grpc server local listening port
	RootCmd.PersistentFlags().IntVar(&config.API.GrpcServerPort, "grpc-port",
		config.API.GrpcServerPort, "GRPC api server port")

	/**========================Consensus Flags ========================== **/
	//todo: add this here

	RootCmd.AddCommand(VersionCmd)

	// Bind Flags to config
	viper.BindPFlags(RootCmd.PersistentFlags())

}
