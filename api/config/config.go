package config

// ConfigValues provide default config values.
var ConfigValues = Config{
	StartGrpcServer: false, // note: all bool flags default to false so don't set one of these to true here
	GrpcServerPort:  9091,
	StartJSONServer: false,
	JSONServerPort:  9090,
}

func init() {
	// todo: update default config params based on runtime env here
}

// Config defines the api config params.
type Config struct {
	StartGrpcServer bool
	GrpcServerPort  int
	StartJSONServer bool
	JSONServerPort  int
}
