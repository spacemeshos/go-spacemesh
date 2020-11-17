// Package config provides configuration for GRPC and HTTP api servers
package config

import (
	"errors"
)

const (
	defaultStartGRPCServer         = false
	defaultGRPCServerPort          = 19091
	defaultNewGRPCServerPort       = 19092
	defaultNewGRPCServerInterface  = ""
	defaultStartJSONServer         = false
	defaultStartNewJSONServer      = false
	defaultJSONServerPort          = 19090
	defaultNewJSONServerPort       = 19093
	defaultStartDebugService       = false
	defaultStartGatewayService     = false
	defaultStartGlobalStateService = false
	defaultStartMeshService        = false
	defaultStartNodeService        = false
	defaultStartSmesherService     = false
	defaultStartTransactionService = false
)

// Config defines the api config params
type Config struct {
	StartGrpcServer        bool     `mapstructure:"grpc-server"`
	StartGrpcServices      []string `mapstructure:"grpc"`
	GrpcServerPort         int      `mapstructure:"grpc-port"`
	NewGrpcServerPort      int      `mapstructure:"grpc-port-new"`
	NewGrpcServerInterface string   `mapstructure:"grpc-interface-new"`
	StartJSONServer        bool     `mapstructure:"json-server"`
	StartNewJSONServer     bool     `mapstructure:"json-server-new"`
	JSONServerPort         int      `mapstructure:"json-port"`
	NewJSONServerPort      int      `mapstructure:"json-port-new"`
	// no direct command line flags for these
	StartDebugService       bool
	StartGatewayService     bool
	StartGlobalStateService bool
	StartMeshService        bool
	StartNodeService        bool
	StartSmesherService     bool
	StartTransactionService bool
}

func init() {
	// todo: update default config params based on runtime env here
}

// DefaultConfig defines the default configuration options for api
func DefaultConfig() Config {
	return Config{
		StartGrpcServer:         defaultStartGRPCServer, // note: all bool flags default to false so don't set one of these to true here
		StartGrpcServices:       nil,                    // note: cannot configure an array as a const
		GrpcServerPort:          defaultGRPCServerPort,
		NewGrpcServerPort:       defaultNewGRPCServerPort,
		NewGrpcServerInterface:  defaultNewGRPCServerInterface,
		StartJSONServer:         defaultStartJSONServer,
		StartNewJSONServer:      defaultStartNewJSONServer,
		JSONServerPort:          defaultJSONServerPort,
		NewJSONServerPort:       defaultNewJSONServerPort,
		StartDebugService:       defaultStartDebugService,
		StartGatewayService:     defaultStartGatewayService,
		StartGlobalStateService: defaultStartGlobalStateService,
		StartMeshService:        defaultStartMeshService,
		StartNodeService:        defaultStartNodeService,
		StartSmesherService:     defaultStartSmesherService,
		StartTransactionService: defaultStartTransactionService,
	}
}

// ParseServicesList enables the requested services
func (s *Config) ParseServicesList() error {
	// Make sure all enabled GRPC services are known
	for _, svc := range s.StartGrpcServices {
		switch svc {
		case "debug":
			s.StartDebugService = true
		case "gateway":
			s.StartGatewayService = true
		case "globalstate":
			s.StartGlobalStateService = true
		case "mesh":
			s.StartMeshService = true
		case "node":
			s.StartNodeService = true
		case "smesher":
			s.StartSmesherService = true
		case "transaction":
			s.StartTransactionService = true
		default:
			return errors.New("unrecognized GRPC service requested: " + svc)
		}
	}

	// If JSON gateway server is enabled, make sure at least one
	// GRPC service is also enabled
	if s.StartNewJSONServer &&
		!s.StartDebugService &&
		!s.StartGatewayService &&
		!s.StartGlobalStateService &&
		!s.StartMeshService &&
		!s.StartNodeService &&
		!s.StartSmesherService &&
		!s.StartTransactionService &&
		// 'true' keeps the above clean
		true {
		return errors.New("must enable at least one GRPC service along with JSON gateway service")
	}

	return nil
}
