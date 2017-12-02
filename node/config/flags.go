package config

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	KSecurityFlag = cli.UintFlag{
		Name:  "k",
		Usage: "Consensus protocol k security param",
		Value: DefaultConfig.SecurityParam,
	}

	LocalTcpPort = cli.UintFlag{
		Name:  "tcp-port, tcp-p",
		Usage: "tcp port to listen on",
		Value: DefaultConfig.TcpPort,
	}
)
