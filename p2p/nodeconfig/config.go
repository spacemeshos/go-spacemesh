package nodeconfig

import (
	"gopkg.in/urfave/cli.v1"
	"log"
	"time"
)

// Default node config values
var ConfigValues = Config{
	SecurityParam: 20,
	FastSync:      true,
	TcpPort:       7513,
	NodeId:        "",
	DialTimeout:   duration{"1m"},
	ConnKeepAlive: duration{"48h"},
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

type duration struct {
	string
}

func (d *duration) Duration() (duration time.Duration) {
	dur, err := time.ParseDuration(d.string)
	if err != nil {
		log.Fatal("Could'nt parse duration string returning 0, error: %v", err)
	}
	return dur
}

type Config struct {
	SecurityParam uint
	FastSync      bool
	TcpPort       int
	NodeId        string
	DialTimeout   duration
	ConnKeepAlive duration
	SwarmConfig   SwarmConfig
}

type SwarmConfig struct {
	Bootstrap              bool
	RoutingTableBucketSize int
	RoutingTableAlpha      int
	RandomConnections      int
	BootstrapNodes         cli.StringSlice
}
