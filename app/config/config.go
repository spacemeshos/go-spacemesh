package config

// Provide Config struct with default values

// Config values with defaults
var ConfigValues = Config{
	AppIntParam:    20,
	AppBoolParam:   true,
	ConfigFilePath: "$GOPATH/src/github.com/spacemeshos/go-spacemesh/config.toml",
	DataFilePath:   "~/.spacemesh",
}

func init() {
	// todo: update default config params based on runtime env here
}

type Config struct {
	ConfigFilePath string
	AppIntParam    int
	AppBoolParam   bool
	DataFilePath   string
}
