package config

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

	LocalTcpPort = cli.UintFlag{
		Name:        "tcp-port, p",
		Usage:       "tcp port to listen on",
		Value:       ConfigValues.TcpPort,
		Destination: &ConfigValues.TcpPort,
	}
)
