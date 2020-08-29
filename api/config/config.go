// Package config provides configuration for GRPC and HTTP api servers
package config

import (
	"errors"
)

const (
	defaultGRPCServerPort          = 9092
	defaultGRPCServerInterface     = ""
	defaultStartJSONServer         = false
	defaultJSONServerPort          = 9093
	defaultStartNodeService        = false
	defaultStartMeshService        = false
	defaultStartGlobalStateService = false
	defaultStartTransactionService = false
	defaultStartSmesherService     = false
)

// Config defines the api config params
type Config struct {
	StartGrpcServices   []string `mapstructure:"grpc"`
	GrpcServerPort      int      `mapstructure:"grpc-port"`
	GrpcServerInterface string   `mapstructure:"grpc-interface"`
	StartJSONServer     bool     `mapstructure:"json-server"`
	JSONServerPort      int      `mapstructure:"json-port"`
	// no direct command line flags for these
	StartNodeService        bool
	StartMeshService        bool
	StartGlobalStateService bool
	StartTransactionService bool
	StartSmesherService     bool
}

func init() {
	// todo: update default config params based on runtime env here
}

// DefaultConfig defines the default configuration options for api
func DefaultConfig() Config {
	return Config{
		// note: all bool flags default to false so don't set one of these to true here
		StartGrpcServices:       nil, // note: cannot configure an array as a const
		GrpcServerPort:          defaultGRPCServerPort,
		GrpcServerInterface:     defaultGRPCServerInterface,
		StartJSONServer:         defaultStartJSONServer,
		JSONServerPort:          defaultJSONServerPort,
		StartNodeService:        defaultStartNodeService,
		StartMeshService:        defaultStartMeshService,
		StartGlobalStateService: defaultStartGlobalStateService,
		StartTransactionService: defaultStartTransactionService,
		StartSmesherService:     defaultStartSmesherService,
	}
}

// ParseServicesList enables the requested services
func (s *Config) ParseServicesList() error {
	// Make sure all enabled GRPC services are known
	for _, svc := range s.StartGrpcServices {
		switch svc {
		case "mesh":
			s.StartMeshService = true
		case "node":
			s.StartNodeService = true
		case "globalstate":
			s.StartGlobalStateService = true
		case "transaction":
			s.StartTransactionService = true
		case "smesher":
			s.StartSmesherService = true
		default:
			return errors.New("unrecognized GRPC service requested: " + svc)
		}
	}

	// If JSON gateway server is enabled, make sure at least one
	// GRPC service is also enabled
	if s.StartJSONServer && !s.StartNodeService && !s.StartMeshService &&
		!s.StartGlobalStateService && !s.StartTransactionService && !s.StartSmesherService {
		return errors.New("must enable at least one GRPC service along with JSON gateway service")
	}

	return nil
}
