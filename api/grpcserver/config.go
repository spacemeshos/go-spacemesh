// Package config provides configuration for GRPC and HTTP api servers
package grpcserver

import (
	"testing"
	"time"
)

type Config struct {
	PublicServices         []Service
	PublicListener         string `mapstructure:"grpc-public-listener"`
	PrivateServices        []Service
	PrivateListener        string `mapstructure:"grpc-private-listener"`
	PostServices           []Service
	PostListener           string    `mapstructure:"grpc-post-listener"`
	TLSServices            []Service `mapstructure:"grpc-tls-services"`
	TLSListener            string    `mapstructure:"grpc-tls-listener"`
	TLSCACert              string    `mapstructure:"grpc-tls-ca-cert"`
	TLSCert                string    `mapstructure:"grpc-tls-cert"`
	TLSKey                 string    `mapstructure:"grpc-tls-key"`
	GrpcSendMsgSize        int       `mapstructure:"grpc-send-msg-size"`
	GrpcRecvMsgSize        int       `mapstructure:"grpc-recv-msg-size"`
	JSONListener           string    `mapstructure:"grpc-json-listener"`
	JSONCorsAllowedOrigins []string  `mapstructure:"grpc-cors-allowed-origins"`

	SmesherStreamInterval time.Duration `mapstructure:"smesherstreaminterval"`

	DatabaseConnections int `mapstructure:"db-connections"`
}

type Service = string

const (
	Admin                     Service = "admin"
	Debug                     Service = "debug"
	GlobalState               Service = "global"
	Mesh                      Service = "mesh"
	Transaction               Service = "transaction"
	Activation                Service = "activation"
	Smesher                   Service = "smesher"
	Post                      Service = "post"
	PostInfo                  Service = "postInfo"
	Node                      Service = "node"
	ActivationV2Alpha1        Service = "activation_v2alpha1"
	ActivationStreamV2Alpha1  Service = "activation_stream_v2alpha1"
	RewardV2Alpha1            Service = "reward_v2alpha1"
	RewardStreamV2Alpha1      Service = "reward_stream_v2alpha1"
	NetworkV2Alpha1           Service = "network_v2alpha1"
	NodeV2Alpha1              Service = "node_v2alpha1"
	LayerV2Alpha1             Service = "layer_v2alpha1"
	LayerStreamV2Alpha1       Service = "layer_stream_v2alpha1"
	TransactionV2Alpha1       Service = "transaction_v2alpha1"
	TransactionStreamV2Alpha1 Service = "transaction_stream_v2alpha1"
	AccountV2Alpha1           Service = "account_v2alpha1"
	MalfeasanceV2Alpha1       Service = "malfeasance_v2alpha1"
	MalfeasanceStreamV2Alpha1 Service = "malfeasance_stream_v2alpha1"

	// v2beta1.
	ActivationV2Beta1        Service = "activation_v2beta1"
	ActivationStreamV2Beta1  Service = "activation_stream_v2beta1"
	RewardV2Beta1            Service = "reward_v2beta1"
	RewardStreamV2Beta1      Service = "reward_stream_v2beta1"
	NetworkV2Beta1           Service = "network_v2beta1"
	NodeV2Beta1              Service = "node_v2beta1"
	LayerV2Beta1             Service = "layer_v2beta1"
	LayerStreamV2Beta1       Service = "layer_stream_v2beta1"
	TransactionV2Beta1       Service = "transaction_v2beta1"
	TransactionStreamV2Beta1 Service = "transaction_stream_v2beta1"
	AccountV2Beta1           Service = "account_v2beta1"
	MalfeasanceV2Beta1       Service = "malfeasance_v2beta1"
	MalfeasanceStreamV2Beta1 Service = "malfeasance_stream_v2beta1"
)

// DefaultConfig defines the default configuration options for api.
func DefaultConfig() Config {
	return Config{
		PublicServices: []Service{
			// v1
			GlobalState, Mesh, Transaction, Node, Activation,

			// v2alpha1
			ActivationV2Alpha1, RewardV2Alpha1, NetworkV2Alpha1, NodeV2Alpha1,
			LayerV2Alpha1, TransactionV2Alpha1, AccountV2Alpha1, MalfeasanceV2Alpha1,

			// v2beta1
			ActivationV2Beta1, RewardV2Beta1, NetworkV2Beta1, NodeV2Beta1,
			LayerV2Beta1, TransactionV2Beta1, AccountV2Beta1, MalfeasanceV2Beta1,
		},
		PublicListener: "0.0.0.0:9092",
		PrivateServices: []Service{
			// v1
			Admin, Smesher, Debug,

			// v2alpha1
			ActivationStreamV2Alpha1, RewardStreamV2Alpha1, LayerStreamV2Alpha1,
			TransactionStreamV2Alpha1, MalfeasanceStreamV2Alpha1,

			// v2beta1
			ActivationStreamV2Beta1, RewardStreamV2Beta1, LayerStreamV2Beta1,
			TransactionStreamV2Beta1, MalfeasanceStreamV2Beta1,
		},
		PrivateListener:        "127.0.0.1:9093",
		PostServices:           []Service{Post, PostInfo},
		PostListener:           "127.0.0.1:0",
		TLSServices:            []Service{Post, PostInfo},
		TLSListener:            "",
		JSONListener:           "",
		JSONCorsAllowedOrigins: []string{""},
		GrpcSendMsgSize:        1024 * 1024 * 10,
		GrpcRecvMsgSize:        1024 * 1024 * 10,
		SmesherStreamInterval:  time.Second,
		DatabaseConnections:    16,
	}
}

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig(tb testing.TB) Config {
	conf := DefaultConfig()
	conf.PublicListener = "127.0.0.1:0"
	conf.PrivateListener = "127.0.0.1:0"
	conf.PostListener = "127.0.0.1:0"
	conf.JSONListener = ""
	conf.TLSListener = ""
	return conf
}
