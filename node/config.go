package node



// Provide Config struct with default values - only 1 not several

// Implement logic to override Configs from command line
var DefaultConfig = Config{
	SecurityParam: 20,
	FastSync: true,
}

func init () {
	// set default config params based on runtim here
}

type Config struct {
	SecurityParam uint `toml:"-"`
	FastSync      bool `toml:"-"`
}
