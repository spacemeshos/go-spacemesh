package config

import (
	"gopkg.in/urfave/cli.v1"
)

var (
	LoadConfigFileFlag = cli.StringFlag{
		Name:        "config, c",
		Usage:       "Load configuration from `FILE`",
		Value:       ConfigValues.ConfigFilePath,
		Destination: &ConfigValues.ConfigFilePath,
	}

	DataFolderPathFlag = cli.StringFlag{
		Name:        "dataFolder, df",
		Usage:       "Set root data folder`",
		Value:       ConfigValues.DataFilePath,
		Destination: &ConfigValues.DataFilePath,
	}

	StartJsonApiServer = cli.BoolFlag{
		Name:        "jsonApiServer, jrpc",
		Usage:       "Start the json http server. Note that starting the Json server also starts the grpc server.",
		Destination: &ConfigValues.StartJsonServer,
	}

	JsonServerPort = cli.UintFlag{
		Name:        "jsonPort, jport",
		Usage:       "Json api server port",
		Value:       ConfigValues.JsonServerPort,
		Destination: &ConfigValues.JsonServerPort,
	}

	StartGrpcApiServer = cli.BoolFlag{
		Name:        "grpcApiServer, grpc",
		Usage:       "Start the grpc server",
		Destination: &ConfigValues.StartGrpcServer,
	}

	GrpcServerPort = cli.UintFlag{
		Name:        "grpcPort, gport",
		Usage:       "Grpc api server port",
		Value:       ConfigValues.GrpcServerPort,
		Destination: &ConfigValues.GrpcServerPort,
	}
)
