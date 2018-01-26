package nodeconfig

import (
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
)

var (
	// KSecurityFlag is the security param to start the app with
	KSecurityFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:  "security-param",
		Usage: "Consensus protocol k security param",
		// use Destination and not value so the app will automaically update the default values
		Value:       ConfigValues.SecurityParam,
		Destination: &ConfigValues.SecurityParam,
	})

	// LocalTCPPortFlag is the local tcp port that the app will listen to
	LocalTCPPortFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "tcp-port",
		Usage:       "tcp port to listen on",
		Value:       ConfigValues.TCPPort,
		Destination: &ConfigValues.TCPPort,
	})

	// NetworkDialTimeout is the dial timeout for the node
	NetworkDialTimeout = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "dial-timeout",
		Usage:       "network dial timeout duration",
		Value:       ConfigValues.DialTimeout.string,
		Destination: &ConfigValues.DialTimeout.string,
	})

	// NetworkConnKeepAlive is the time that we'll keep alive the connection
	NetworkConnKeepAlive = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "conn-keepalive",
		Usage:       "Network connection keep alive",
		Value:       ConfigValues.ConnKeepAlive.string,
		Destination: &ConfigValues.ConnKeepAlive.string,
	})

	// NodeIDFlag is holding our node id
	NodeIDFlag = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "node-id",
		Usage:       "Load node data by id (pub key) from local store",
		Value:       ConfigValues.NodeID,
		Destination: &ConfigValues.NodeID,
	})

	// SwarmBootstrap is flag to bootstrap the swarm
	SwarmBootstrap = altsrc.NewBoolFlag(cli.BoolFlag{
		Name:        "swarm-bootstrap",
		Usage:       "Bootstrap the swarm",
		Destination: &SwarmConfigValues.Bootstrap,
	})

	// RoutingTableBucketSizdFlag will determine the swarm rounding table bucket size
	RoutingTableBucketSizdFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "swarm-rtbs",
		Usage:       "The rounding table bucket size",
		Value:       SwarmConfigValues.RoutingTableBucketSize,
		Destination: &SwarmConfigValues.RoutingTableBucketSize,
	})

	// RoutingTableAlphaFlag will determine the swarm table routing alpha
	RoutingTableAlphaFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "swarm-rtalpha",
		Usage:       "The rounding table Alpha",
		Value:       SwarmConfigValues.RoutingTableAlpha,
		Destination: &SwarmConfigValues.RoutingTableAlpha,
	})

	// RandomConnectionsFlag will determine how much random connection the swarm have
	RandomConnectionsFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "swarm-randcon",
		Usage:       "Number of random connections",
		Value:       SwarmConfigValues.RandomConnections,
		Destination: &SwarmConfigValues.RandomConnections,
	})

	// BootstrapNodesFlag holds an array of nodes the will be use to bootstrap the spacemesh ndoe
	BootstrapNodesFlag = altsrc.NewStringSliceFlag(cli.StringSliceFlag{
		Name:  "swarm-bootstrap-nodes",
		Usage: "Foundation nodes to bootstrap spacemesh",
		Value: &SwarmConfigValues.BootstrapNodes,
	})
)
