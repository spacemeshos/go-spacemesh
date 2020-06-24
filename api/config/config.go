// Package config provides configuration for GRPC and HTTP api servers
package config

import (
	"errors"
)

const (
	defaultStartGRPCServer    = false
	defaultGRPCServerPort     = 9091
	defaultNewGRPCServerPort  = 9092
	defaultStartJSONServer    = false
	defaultStartNewJSONServer = false
	defaultJSONServerPort     = 9090
	defaultNewJSONServerPort  = 9093
	defaultStartNodeService   = false
)

// Config defines the api config params
type Config struct {
	StartGrpcServer    bool     `mapstructure:"grpc-server"`
	StartGrpcServices  []string `mapstructure:"grpc"`
	GrpcServerPort     int      `mapstructure:"grpc-port"`
	NewGrpcServerPort  int      `mapstructure:"grpc-port-new"`
	StartJSONServer    bool     `mapstructure:"json-server"`
	StartNewJSONServer bool     `mapstructure:"json-server-new"`
	JSONServerPort     int      `mapstructure:"json-port"`
	NewJSONServerPort  int      `mapstructure:"json-port-new"`
	StartNodeService   bool     // no direct commandline flag
}

func init() {
	// todo: update default config params based on runtime env here
}

// DefaultConfig defines the default configuration options for api
func DefaultConfig() Config {
	return Config{
		StartGrpcServer:    defaultStartGRPCServer, // note: all bool flags default to false so don't set one of these to true here
		StartGrpcServices:  nil,                    // note: cannot configure an array as a const
		GrpcServerPort:     defaultGRPCServerPort,
		NewGrpcServerPort:  defaultNewGRPCServerPort,
		StartJSONServer:    defaultStartJSONServer,
		StartNewJSONServer: defaultStartNewJSONServer,
		JSONServerPort:     defaultJSONServerPort,
		NewJSONServerPort:  defaultNewJSONServerPort,
		StartNodeService:   defaultStartNodeService,
	}
}

// ParseServicesList enables the requested services
func (s *Config) ParseServicesList() error {
	for _, svc := range s.StartGrpcServices {
		switch svc {
		case "node":
			s.StartNodeService = true
		default:
			return errors.New("Unrecognized GRPC service requested: " + svc)
		}
	}
	return nil
}
