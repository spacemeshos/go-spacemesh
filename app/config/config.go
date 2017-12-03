package config

// Provide Config struct with default values

// Config values with defaults
var ConfigValues = Config {
	AppIntParam:     20,
	AppBoolParam:    true,
	DataFilePath:    "~/.unruly",
	StartGrpcServer: false,			// note: all bool flags default to false so don't set one of these to true here
	StartJsonServer: false,
}

func init() {
	// todo: update default config params based on runtime env here
}

type Config struct {
	ConfigFilePath  string `toml:"-"`
	AppIntParam     int    `toml:"-"`
	AppBoolParam    bool   `toml:"-"`
	DataFilePath    string `toml:"-"`
	StartGrpcServer bool   `toml:"-"`
	StartJsonServer bool   `toml:"-"`
}
