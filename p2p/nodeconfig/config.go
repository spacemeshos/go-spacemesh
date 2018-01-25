package nodeconfig

import "time"

// ConfigValues specifies  default values for node config params
var ConfigValues = Config{
	SecurityParam: 20,
	FastSync:      true,
	TCPPort:       7513,
	NodeID:        "",
	DialTimeout:   time.Duration(1 * time.Minute),
	ConnKeepAlive: time.Duration(48 * time.Hour),
	SwarmConfig:   SwarmConfigValues,
}

// SwarmConfigValues defines default values for swarm config params
var SwarmConfigValues = SwarmConfig{
	Bootstrap:              false,
	RoutingTableBucketSize: 20,
	RoutingTableAlpha:      3,
	RandomConnections:      5,
	BootstrapNodes: []string{ // these should be the spacemesh foundation bootstrap nodes
		"125.0.0.1:3572/iaMujEYTByKcjMZWMqg79eJBGMDm8ADsWZFdouhpfeKj",
		"125.0.0.1:3763/x34UDdiCBAsXmLyMMpPQzs313B9UDeHNqFpYsLGfaFvm",
	},
}

func init() {
	// set default config params based on runtime here
}

// Config specifies node config params
type Config struct {
	SecurityParam uint          `toml:"-"`
	FastSync      bool          `toml:"-"`
	TCPPort       uint          `toml:"-"`
	NodeID        string        `toml:"-"`
	DialTimeout   time.Duration `toml:"-"`
	ConnKeepAlive time.Duration `toml:"-"`
	SwarmConfig   SwarmConfig   `toml:"-"`
}

// SwarmConfig specifies swarm config params
type SwarmConfig struct {
	Bootstrap              bool     `toml:"-"`
	RoutingTableBucketSize uint     `toml:"-"`
	RoutingTableAlpha      uint     `toml:"-"`
	RandomConnections      uint     `toml:"-"`
	BootstrapNodes         []string `toml:"-"`
}
