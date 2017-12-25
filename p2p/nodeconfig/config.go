package nodeconfig

import "time"

// Default node config values
var ConfigValues = Config{
	SecurityParam: 20,
	FastSync:      true,
	TcpPort:       7513,
	NodeId:        "",
	DialTimeout:   time.Duration(1 * time.Minute),
	ConnKeepAlive: time.Duration(48 * time.Hour),
	SwarmConfig:   SwarmConfigValues,
}

// todo: make all swamr config values command-line and config file modifiable
var SwarmConfigValues = SwarmConfig{
	Bootstrap:              false,
	RoutingTableBucketSize: 20,
	RoutingTableAlpha:      3,
	RandomConnections:      5,
	BootstrapNodes: []string{ // these shoul be the unruly foundation bootstrap nodes
		"125.0.0.1:3572/iaMujEYTByKcjMZWMqg79eJBGMDm8ADsWZFdouhpfeKj",
		"125.0.0.1:3763/x34UDdiCBAsXmLyMMpPQzs313B9UDeHNqFpYsLGfaFvm",
	},
}

func init() {
	// set default config params based on runtime here
}

type Config struct {
	SecurityParam uint          `toml:"-"`
	FastSync      bool          `toml:"-"`
	TcpPort       uint          `toml:"-"`
	NodeId        string        `toml:"-"`
	DialTimeout   time.Duration `toml:"-"`
	ConnKeepAlive time.Duration `toml:"-"`
	SwarmConfig   SwarmConfig   `toml:"-"`
}

type SwarmConfig struct {
	Bootstrap              bool     `toml:"-"`
	RoutingTableBucketSize uint     `toml:"-"`
	RoutingTableAlpha      uint     `toml:"-"`
	RandomConnections      uint     `toml:"-"`
	BootstrapNodes         []string `toml:"-"`
}
