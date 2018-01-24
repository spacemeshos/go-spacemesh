package nodeconfig

import (
	"gopkg.in/urfave/cli.v1"
	"time"
)

var NodeConfigUints = map[string]*uint{
	"tcp-port":       &ConfigValues.TcpPort,
	"security-param": &ConfigValues.SecurityParam,
	"swarm-rtbs":     &SwarmConfigValues.RoutingTableBucketSize,
	"swarm-rtalpha":  &SwarmConfigValues.RoutingTableAlpha,
	"swarm-randcon":  &SwarmConfigValues.RandomConnections,
}

var NodeConfigDurations = map[string]*time.Duration{
	"dial-timeout":   &ConfigValues.DialTimeout,
	"conn-keepalive": &ConfigValues.ConnKeepAlive,
}

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

// todo: make all swarm config values command-line and config file modifiable
var SwarmConfigValues = SwarmConfig{
	Bootstrap:              false,
	RoutingTableBucketSize: 20,
	RoutingTableAlpha:      3,
	RandomConnections:      5,
	BootstrapNodes: cli.StringSlice{ // these should be the spacemesh foundation bootstrap nodes
		"125.0.0.1:3572/iaMujEYTByKcjMZWMqg79eJBGMDm8ADsWZFdouhpfeKj",
		"125.0.0.1:3763/x34UDdiCBAsXmLyMMpPQzs313B9UDeHNqFpYsLGfaFvm",
	},
}

func init() {
	// set default config params based on runtime here
}

type Config struct {
	SecurityParam uint
	FastSync      bool
	TcpPort       uint
	NodeId        string
	DialTimeout   time.Duration
	ConnKeepAlive time.Duration
	SwarmConfig   SwarmConfig
}

type SwarmConfig struct {
	Bootstrap              bool
	RoutingTableBucketSize uint
	RoutingTableAlpha      uint
	RandomConnections      uint
	BootstrapNodes         cli.StringSlice
}
