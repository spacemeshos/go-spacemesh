package config

const (
	defaultStartGRPCServer = false
	defaultGRPCServerPort  = 9091
	defaultStartJSONServer = false
	defaultJSONServerPort  = 9090
)

// Config defines the api config params
type Config struct {
	StartGrpcServer bool `mapstructure:"grpc-server"`
	GrpcServerPort  int  `mapstructure:"grpc-port"`
	StartJSONServer bool `mapstructure:"json-server"`
	JSONServerPort  int  `mapstructure:"json-port"`
}

// ConfigValues set default values
var ConfigValues = DefaultConfig()

func init() {
	// todo: update default config params based on runtime env here
}

// DefaultConfig defines the default configuration options for api
func DefaultConfig() Config {
	return Config{
		StartGrpcServer: defaultStartGRPCServer, // note: all bool flags default to false so don't set one of these to true here
		GrpcServerPort:  defaultGRPCServerPort,
		StartJSONServer: defaultStartJSONServer,
		JSONServerPort:  defaultJSONServerPort,
	}
}
