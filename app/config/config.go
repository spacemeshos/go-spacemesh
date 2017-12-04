package config

// Provide Config struct with default values

// Config values with defaults
var ConfigValues = Config{
	AppIntParam:     20,
	AppBoolParam:    true,
	DataFilePath:    "~/.unruly",
}

func init() {
	// todo: update default config params based on runtime env here
}

type Config struct {
	ConfigFilePath  string `toml:"-"`
	AppIntParam     int    `toml:"-"`
	AppBoolParam    bool   `toml:"-"`
	DataFilePath    string `toml:"-"`
}
