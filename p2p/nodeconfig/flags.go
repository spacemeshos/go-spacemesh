package nodeconfig

import (
	"gopkg.in/urfave/cli.v1"
)

// Node flags
var (
	KSecurityFlag = cli.UintFlag{
		Name:  "k",
		Usage: "Consensus protocol k security param",
		// use Destination and not value so the app will automaically update the default values
		Value:       ConfigValues.SecurityParam,
		Destination: &ConfigValues.SecurityParam,
	}

	LocalTCPPortFlag = cli.UintFlag{
		Name:        "tcp-port, p",
		Usage:       "tcp port to listen on",
		Value:       ConfigValues.TCPPort,
		Destination: &ConfigValues.TCPPort,
	}

	NetworkDialTimeout = cli.DurationFlag{
		Name:        "dial-timeout, d",
		Usage:       "network dial timeout duration",
		Value:       ConfigValues.DialTimeout,
		Destination: &ConfigValues.DialTimeout,
	}

	NetworkConnKeepAlive = cli.DurationFlag{
		Name:        "conn-keepalive, ka",
		Usage:       "Network connection keep alive",
		Value:       ConfigValues.ConnKeepAlive,
		Destination: &ConfigValues.ConnKeepAlive,
	}

	NodeIDFlag = cli.StringFlag{
		Name:        "nodeId, n",
		Usage:       "Load node data by id (pub key) from local store",
		Value:       ConfigValues.NodeID,
		Destination: &ConfigValues.NodeID,
	}
)
