package cmd

import (
	"fmt"
	cfg "github.com/spacemeshos/go-spacemesh/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
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
	addCommands(RootCmd)
}

func addCommands(cmd *cobra.Command) {

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
	cmd.PersistentFlags().StringVar(&config.GenesisTime, "genesis-time",
		config.GenesisTime, "Time of the genesis layer in 2019-13-02T17:02:00+00:00 format")
	cmd.PersistentFlags().IntVar(&config.LayerDurationSec, "layer-duration-sec",
		config.LayerDurationSec, "Duration between layers in seconds")
	/** ======================== P2P Flags ========================== **/
	cmd.PersistentFlags().IntVar(&config.P2P.SecurityParam, "security-param",
		config.P2P.SecurityParam, "Consensus protocol k security param")
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
	cmd.PersistentFlags().StringVar(&config.P2P.NodeID, "node-id",
		config.P2P.NodeID, "Load node data by id (pub key) from local store")
	cmd.PersistentFlags().BoolVar(&config.P2P.NewNode, "new-node",
		config.P2P.NewNode, "Load node data by id (pub key) from local store")
	cmd.PersistentFlags().IntVar(&config.P2P.BufferSize, "buffer-size",
		config.P2P.BufferSize, "Size of the messages handler's buffer")
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
	cmd.PersistentFlags().DurationVar(&config.HARE.RoundDuration, "hare-round-duration-ms",
		config.HARE.RoundDuration, "Duration of round in the Hare protocol")

	/**========================Consensus Flags ========================== **/

	// Bind Flags to config
	viper.BindPFlags(cmd.PersistentFlags())

}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}
