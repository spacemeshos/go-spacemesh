package config

// Provide Config struct with default values

// ConfigValues defines default values for app config params.
var ConfigValues = Config{
	AppIntParam:  20,
	AppBoolParam: true,
	DataFilePath: "~/spacemesh/",
}

func init() {
	// todo: update default config params based on runtime env here
}

// Config defines app config params.
type Config struct {
	ConfigFilePath string
	AppIntParam    int
	AppBoolParam   bool
	DataFilePath   string
}
