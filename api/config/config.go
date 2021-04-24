// Package config provides configuration for GRPC and HTTP api servers
package config

import (
	"errors"
	"fmt"
)

const (
	defaultGRPCServerPort          = 9092
	defaultGRPCServerInterface     = ""
	defaultStartJSONServer         = false
	defaultJSONServerPort          = 9093
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
	StartGrpcServices   []string `mapstructure:"grpc"`
	GrpcServerPort      int      `mapstructure:"grpc-port"`
	GrpcServerInterface string   `mapstructure:"grpc-interface"`
	StartJSONServer     bool     `mapstructure:"json-server"`
	JSONServerPort      int      `mapstructure:"json-port"`
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
		// note: all bool flags default to false so don't set one of these to true here
		StartGrpcServices:       nil, // note: cannot configure an array as a const
		GrpcServerPort:          defaultGRPCServerPort,
		GrpcServerInterface:     defaultGRPCServerInterface,
		StartJSONServer:         defaultStartJSONServer,
		JSONServerPort:          defaultJSONServerPort,
		StartDebugService:       defaultStartDebugService,
		StartGatewayService:     defaultStartGatewayService,
		StartGlobalStateService: defaultStartGlobalStateService,
		StartMeshService:        defaultStartMeshService,
		StartNodeService:        defaultStartNodeService,
		StartSmesherService:     defaultStartSmesherService,
		StartTransactionService: defaultStartTransactionService,
	}
}

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig() Config {
	testPortOffset := 10000
	conf := DefaultConfig()
	conf.GrpcServerPort += testPortOffset
	conf.JSONServerPort += testPortOffset
	return conf
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
			return fmt.Errorf("unrecognized GRPC service requested: %s", svc)
		}
	}

	// If JSON gateway server is enabled, make sure at least one
	// GRPC service is also enabled
	if s.StartJSONServer &&
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
