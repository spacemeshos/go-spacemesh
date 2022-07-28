package address

// Config is the configuration of the address package.
type Config struct {
	NetworkName string `mapstructure:"network-name"`
	NetworkHRP  string `mapstructure:"network-hrp"`
}

var conf = &Config{
	NetworkHRP:  "sm",
	NetworkName: "spacemesh network",
}

// DefaultAddressConfig returns the default configuration of the address package.
func DefaultAddressConfig() *Config {
	return conf
}

// DefaultTestAddressConfig returns the default test configuration of the address package.
func DefaultTestAddressConfig() *Config {
	conf = &Config{
		NetworkHRP:  "smt",
		NetworkName: "test spacemesh network",
	}
	return conf
}
