package nodeconfig

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	KSecurityFlag = cli.UintFlag{
		Name:  "k",
		Usage: "Consensus protocol k security param",
		// use Destination and not value so the app will automaically update the default values
		Value:       ConfigValues.SecurityParam,
		Destination: &ConfigValues.SecurityParam,
	}

	LocalTcpPortFlag = cli.UintFlag{
		Name:        "tcp-port, p",
		Usage:       "tcp port to listen on",
		Value:       ConfigValues.TcpPort,
		Destination: &ConfigValues.TcpPort,
	}

	NodeIdFlag = cli.StringFlag{
		Name:        "nodeId, n",
		Usage:       "Load node data by id (pub key) from local store",
		Value:       ConfigValues.NodeId,
		Destination: &ConfigValues.NodeId,
	}
)
