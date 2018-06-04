package config

import (
	"gopkg.in/urfave/cli.v1"
	"gopkg.in/urfave/cli.v1/altsrc"
)

// Service flags for api servers.
var (
	// StartJSONApiServerFlag determines if json api server should be started.
	StartJSONApiServerFlag = altsrc.NewBoolFlag(cli.BoolFlag{
		Name:        "json-server",
		Usage:       "StartService the json http server. Note that starting the Json server also starts the grpc server.",
		Destination: &ConfigValues.StartJSONServer,
	})

	// JSONServerPortFlag determines the json api server local listening port.
	JSONServerPortFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "json-port",
		Usage:       "Json api server port",
		Value:       ConfigValues.JSONServerPort,
		Destination: &ConfigValues.JSONServerPort,
	})

	// StartGrpcAPIServerFlag determines if the grpc server should be started.
	StartGrpcAPIServerFlag = altsrc.NewBoolFlag(cli.BoolFlag{
		Name:        "grpc-server",
		Usage:       "StartService the grpc server",
		Destination: &ConfigValues.StartGrpcServer,
	})

	// GrpcServerPortFlag determines the grpc server local listening port.
	GrpcServerPortFlag = altsrc.NewIntFlag(cli.IntFlag{
		Name:        "grpc-port",
		Usage:       "Grpc api server port",
		Value:       ConfigValues.GrpcServerPort,
		Destination: &ConfigValues.GrpcServerPort,
	})
)
