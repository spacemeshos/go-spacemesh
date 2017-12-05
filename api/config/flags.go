package config

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	StartJsonApiServerFlag = cli.BoolFlag{
		Name:        "jsonApiServer, jrpc",
		Usage:       "StartService the json http server. Note that starting the Json server also starts the grpc server.",
		Destination: &ConfigValues.StartJsonServer,
	}

	JsonServerPortFlag = cli.UintFlag{
		Name:        "jsonPort, jport",
		Usage:       "Json api server port",
		Value:       ConfigValues.JsonServerPort,
		Destination: &ConfigValues.JsonServerPort,
	}

	StartGrpcApiServerFlag = cli.BoolFlag{
		Name:        "grpcApiServer, grpc",
		Usage:       "StartService the grpc server",
		Destination: &ConfigValues.StartGrpcServer,
	}

	GrpcServerPortFlag = cli.UintFlag{
		Name:        "grpcPort, gport",
		Usage:       "Grpc api server port",
		Value:       ConfigValues.GrpcServerPort,
		Destination: &ConfigValues.GrpcServerPort,
	}
)
