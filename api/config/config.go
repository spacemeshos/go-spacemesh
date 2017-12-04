package config

// Provide Config struct with default values

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
	StartGrpcServer bool   `toml:"-"`
	GrpcServerPort  uint   `toml:"-"`
	StartJsonServer bool   `toml:"-"`
	JsonServerPort  uint   `toml:"-"`
}
