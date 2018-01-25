package config

import (
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
)

var (
	StartJsonApiServerFlag = altsrc.NewBoolFlag(cli.BoolFlag{
		Name:        "json-server",
		Usage:       "StartService the json http server. Note that starting the Json server also starts the grpc server.",
		Destination: &ConfigValues.StartJsonServer,
		EnvVar:      "JSON_API_SERVER",
	})

	JsonServerPortFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "json-port",
		Usage:       "JSON API server port",
		Value:       ConfigValues.JsonServerPort,
		Destination: &ConfigValues.JsonServerPort,
		EnvVar:      "JSON_API_PORT",
	})

	StartGrpcApiServerFlag = altsrc.NewBoolFlag(cli.BoolFlag{
		Name:        "grpc-server",
		Usage:       "StartService the gRPC server",
		Destination: &ConfigValues.StartGrpcServer,
		EnvVar:      "GRPC_API_SERVER",
	})

	GrpcServerPortFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "grpc-port",
		Usage:       "The gRPC API server port",
		Value:       ConfigValues.GrpcServerPort,
		Destination: &ConfigValues.GrpcServerPort,
		EnvVar:      "GRPC_API_PORT",
	})
)
