// Package config provides configuration for GRPC and HTTP api servers
package grpcserver

import (
	"time"
)

type Config struct {
	PublicServices  []Service `mapstructure:"grpc-public-services"`
	PublicListener  string    `mapstructure:"grpc-public-listener"`
	PrivateServices []Service `mapstructure:"grpc-private-services"`
	PrivateListener string    `mapstructure:"grpc-private-listener"`
	GrpcSendMsgSize int       `mapstructure:"grpc-send-msg-size"`
	GrpcRecvMsgSize int       `mapstructure:"grpc-recv-msg-size"`
	JSONListener    string    `mapstructure:"grpc-json-listener"`

	SmesherStreamInterval time.Duration
}

type Service = string

const (
	Debug       Service = "debug"
	GlobalState Service = "global"
	Mesh        Service = "mesh"
	Transaction Service = "transaction"
	Activation  Service = "activation"
	Smesher     Service = "smesher"
	Node        Service = "node"
)

// DefaultConfig defines the default configuration options for api.
func DefaultConfig() Config {
	return Config{
		PublicServices:        []Service{Debug, GlobalState, Mesh, Transaction, Node},
		PublicListener:        "0.0.0.0:9092",
		PrivateServices:       []Service{Smesher},
		PrivateListener:       "127.0.0.1:9093",
		JSONListener:          "",
		GrpcSendMsgSize:       1024 * 1024 * 10,
		GrpcRecvMsgSize:       1024 * 1024 * 10,
		SmesherStreamInterval: time.Second,
	}
}

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig() Config {
	conf := DefaultConfig()
	conf.PublicListener = "127.0.0.1:19092"
	conf.PrivateListener = "127.0.0.1:19093"
	conf.JSONListener = "127.0.0.1:19094"
	return conf
}
