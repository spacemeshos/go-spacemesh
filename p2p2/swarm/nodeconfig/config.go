package nodeconfig

// config are params with default values that are modifiable via config file or cli flags

// Implement logic to override Configs from command line
var ConfigValues = Config{
	SecurityParam: 20,
	FastSync:      true,
	TcpPort:       7513,
	NodeId:        "",
}

func init() {
	// set default config params based on runtim here
}

type Config struct {
	SecurityParam uint   `toml:"-"`
	FastSync      bool   `toml:"-"`
	TcpPort       uint   `toml:"-"`
	NodeId        string `toml:"-"`
}
