package config

// Provide Config struct with default values

var ApiConfigUints = map[string]*uint{
	"grpc-port": &ConfigValues.GrpcServerPort,
	"json-port": &ConfigValues.JsonServerPort,
}

// Config values with defaults
var ConfigValues = Config{
	StartGrpcServer: false, // note: all bool flags default to false so don't set one of these to true here
	GrpcServerPort:  9091,
	StartJsonServer: false,
	JsonServerPort:  9090,
}

func init() {
	// todo: update default config params based on runtime env here
}

type Config struct {
	StartGrpcServer bool
	GrpcServerPort  uint
	StartJsonServer bool
	JsonServerPort  uint
}