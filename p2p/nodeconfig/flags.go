package nodeconfig

import (
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
)

var (
	KSecurityFlag = altsrc.NewUintFlag(cli.UintFlag{
		Name:  "security-param",
		Usage: "Consensus protocol k security param",
		// use Destination and not value so the app will automaically update the default values
		Value:       ConfigValues.SecurityParam,
		Destination: &ConfigValues.SecurityParam,
	})

	LocalTcpPortFlag = altsrc.NewUintFlag(cli.UintFlag{
		Name:        "tcp-port",
		Usage:       "tcp port to listen on",
		Value:       ConfigValues.TcpPort,
		Destination: &ConfigValues.TcpPort,
	})

	NetworkDialTimeout = altsrc.NewDurationFlag(cli.DurationFlag{
		Name:        "dial-timeout",
		Usage:       "network dial timeout duration",
		Value:       ConfigValues.DialTimeout,
		Destination: &ConfigValues.DialTimeout,
	})

	NetworkConnKeepAlive = altsrc.NewDurationFlag(cli.DurationFlag{
		Name:        "conn-keepalive",
		Usage:       "Network connection keep alive",
		Value:       ConfigValues.ConnKeepAlive,
		Destination: &ConfigValues.ConnKeepAlive,
	})

	NodeIdFlag = altsrc.NewStringFlag(cli.StringFlag{
		Name:        "node-id",
		Usage:       "Load node data by id (pub key) from local store",
		Value:       ConfigValues.NodeId,
		Destination: &ConfigValues.NodeId,
	})

	SwarmBootstrap = altsrc.NewBoolFlag(cli.BoolFlag{
		Name: "swarm-bootstrap",
		Usage: "Bootstrap the swarm",
		Destination: &SwarmConfigValues.Bootstrap,
	})

	RoutingTableBucketSizdFlag = altsrc.NewUintFlag(cli.UintFlag{
		Name: "swarm-rtbs",
		Usage: "The rounding table bucket size",
		Value: SwarmConfigValues.RoutingTableBucketSize,
		Destination:&SwarmConfigValues.RoutingTableBucketSize,
	})

	RoutingTableAlphaFlag = altsrc.NewUintFlag(cli.UintFlag{
		Name: "swarm-rtalpha",
		Usage: "The rounding table Alpha",
		Value: SwarmConfigValues.RoutingTableAlpha,
		Destination:&SwarmConfigValues.RoutingTableAlpha,
	})

	RandomConnectionsFlag = altsrc.NewUintFlag(cli.UintFlag{
		Name: "swarm-randcon",
		Usage: "Number of random connections",
		Value: SwarmConfigValues.RandomConnections,
		Destination:&SwarmConfigValues.RandomConnections,
	})

	BootstrapNodesFlag = altsrc.NewStringSliceFlag(cli.StringSliceFlag{
		Name: "swarm-bootstrap-nodes",
		Usage: "Foundation nodes to bootstrap spacemesh",
		Value: &SwarmConfigValues.BootstrapNodes,
	})



)
