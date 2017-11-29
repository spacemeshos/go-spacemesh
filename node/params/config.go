package params

// config are params with default values that are modifiable via config file or cli flags

// Implement logic to override Configs from command line
var DefaultConfig = Config{
	SecurityParam: 20,
	FastSync:      true,
}

func init() {
	// set default config params based on runtim here
}

type Config struct {
	SecurityParam uint `toml:"-"`
	FastSync      bool `toml:"-"`
}
