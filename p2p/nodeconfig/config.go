package nodeconfig

import "time"

// config are params with default values that are modifiable via config file or cli flags

// Implement logic to override Configs from command line
var ConfigValues = Config {
	SecurityParam: 20,
	FastSync:      true,
	TcpPort:       7513,
	NodeId:        "",
	DialTimeout:   time.Duration(1 * time.Minute),
	ConnKeepAlive: time.Duration(48 * time.Hour),
}

func init() {
	// set default config params based on runtime here
}

type Config struct {
	SecurityParam uint   `toml:"-"`
	FastSync      bool   `toml:"-"`
	TcpPort       uint   `toml:"-"`
	NodeId        string `toml:"-"`
	DialTimeout   time.Duration `toml:"-"`
	ConnKeepAlive time.Duration `toml:"-"`
}
