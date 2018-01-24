package config

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	// StartJSONApiServerFlag determines if json api server should be started
	StartJSONApiServerFlag = cli.BoolFlag{
		Name:        "jsonApiServer, jrpc",
		Usage:       "StartService the json http server. Note that starting the Json server also starts the grpc server.",
		Destination: &ConfigValues.StartJSONServer,
	}

	// JSONServerPortFlag determines the json api server local listening port
	JSONServerPortFlag = cli.UintFlag{
		Name:        "jsonPort, jport",
		Usage:       "Json api server port",
		Value:       ConfigValues.JSONServerPort,
		Destination: &ConfigValues.JSONServerPort,
	}

	// StartGrpcAPIServerFlag determines if the grpc server should be started
	StartGrpcAPIServerFlag = cli.BoolFlag{
		Name:        "grpcApiServer, grpc",
		Usage:       "StartService the grpc server",
		Destination: &ConfigValues.StartGrpcServer,
	}

	// GrpcServerPortFlag determines the grpc server local listening port
	GrpcServerPortFlag = cli.UintFlag{
		Name:        "grpcPort, gport",
		Usage:       "Grpc api server port",
		Value:       ConfigValues.GrpcServerPort,
		Destination: &ConfigValues.GrpcServerPort,
	}
)
