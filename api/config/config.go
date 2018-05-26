package config

const (
	defaultStartGRPCServer = false
	defaultGRPCServerPort = 9091
	defaultStartJSONServer = false
	defaultJSONServerPort = 9090
)

// Config defines the api config params
type Config struct {
	StartGrpcServer bool
	GrpcServerPort  int
	StartJSONServer bool
	JSONServerPort  int
}

var ConfigValues = Config {
	StartGrpcServer: false, // note: all bool flags default to false so don't set one of these to true here
	GrpcServerPort:  9091,
	StartJSONServer: false,
	JSONServerPort:  9090,
}

func init() {
	// todo: update default config params based on runtime env here
}

// DefaultConfig defines the default configuration options for api
func DefaultConfig() *Config {
	return &Config{
		StartGrpcServer: defaultStartGRPCServer, // note: all bool flags default to false so don't set one of these to true here
		GrpcServerPort:  defaultGRPCServerPort,
		StartJSONServer: defaultStartJSONServer,
		JSONServerPort:  defaultJSONServerPort,
	}
}
