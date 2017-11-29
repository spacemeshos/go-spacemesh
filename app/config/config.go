package config

// Provide Config struct with default values - only 1 not several

// Implement logic to override Configs from command line
var DefaultConfig = Config{
	AppIntParam: 20,
	AppBoolParam: true,
}

func init () {
	// set default config params based on runtim here
}

type Config struct {
	ConfigFilePath string `toml:"-"`
	AppIntParam int `toml:"-"`
	AppBoolParam bool `toml:"-"`
}