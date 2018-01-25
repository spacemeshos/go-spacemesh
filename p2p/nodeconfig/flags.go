package nodeconfig

import (
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
)

var (
	KSecurityFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:  "security-param",
		Usage: "Consensus protocol k security param",
		// use Destination and not value so the app will automaically update the default values
		Value:       ConfigValues.SecurityParam,
		Destination: &ConfigValues.SecurityParam,
	})

	LocalTCPPortFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "tcp-port",
		Usage:       "tcp port to listen on",
		Value:       ConfigValues.TCPPort,
		Destination: &ConfigValues.TCPPort,
	})

	NetworkDialTimeout = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "dial-timeout",
		Usage:       "network dial timeout duration",
		Value:       ConfigValues.DialTimeout.string,
		Destination: &ConfigValues.DialTimeout.string,
	})

	NetworkConnKeepAlive = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "conn-keepalive",
		Usage:       "Network connection keep alive",
		Value:       ConfigValues.ConnKeepAlive.string,
		Destination: &ConfigValues.ConnKeepAlive.string,
	})

	NodeIDFlag = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "node-id",
		Usage:       "Load node data by id (pub key) from local store",
		Value:       ConfigValues.NodeID,
		Destination: &ConfigValues.NodeID,
	})

	SwarmBootstrap = altsrc.NewBoolFlag(cli.BoolFlag{
		Name:        "swarm-bootstrap",
		Usage:       "Bootstrap the swarm",
		Destination: &SwarmConfigValues.Bootstrap,
	})

	RoutingTableBucketSizdFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "swarm-rtbs",
		Usage:       "The rounding table bucket size",
		Value:       SwarmConfigValues.RoutingTableBucketSize,
		Destination: &SwarmConfigValues.RoutingTableBucketSize,
	})

	RoutingTableAlphaFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "swarm-rtalpha",
		Usage:       "The rounding table Alpha",
		Value:       SwarmConfigValues.RoutingTableAlpha,
		Destination: &SwarmConfigValues.RoutingTableAlpha,
	})

	RandomConnectionsFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "swarm-randcon",
		Usage:       "Number of random connections",
		Value:       SwarmConfigValues.RandomConnections,
		Destination: &SwarmConfigValues.RandomConnections,
	})

	BootstrapNodesFlag = altsrc.NewStringSliceFlag(cli.StringSliceFlag{
		Name:  "swarm-bootstrap-nodes",
		Usage: "Foundation nodes to bootstrap spacemesh",
		Value: &SwarmConfigValues.BootstrapNodes,
	})
)
