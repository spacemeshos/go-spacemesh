package config

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
)

// ConfigValues specifies  default values for node config params.
var (
	ConfigValues      = DefaultConfig()
)

func init() {
	// set default config params based on runtime here
}

func duration(duration string) (dur time.Duration) {
	dur, err := time.ParseDuration(duration)
	if err != nil {
		log.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

// Config defines the configuration options for the Spacemesh peer-to-peer networking layer
type Config struct {
	TCPPort         int             `mapstructure:"tcp-port"`
	NodeID          string          `mapstructure:"node-id"`
	NewNode         bool            `mapstructure:"new-node"`
	DialTimeout     time.Duration   `mapstructure:"dial-timeout"`
	ConnKeepAlive   time.Duration   `mapstructure:"conn-keepalive"`
	NetworkID       int8            `mapstructure:"network-id"`
	DiscoveryConfig DiscoveryConfig `mapstructure:"dht"`
	BufferSize      int             `mapstructure:"buffer-size"`
}

// DiscoveryConfig specifies swarm config params.
type DiscoveryConfig struct {
	Gossip                 bool     `mapstructure:"gossip"`
	Bootstrap              bool     `mapstructure:"bootstrap"`
	RoutingTableBucketSize int      `mapstructure:"bucketsize"`
	RoutingTableAlpha      int      `mapstructure:"alpha"`
	RandomConnections      int      `mapstructure:"randcon"`
	BootstrapNodes         []string `mapstructure:"bootnodes"`
}

// DefaultConfig deines the default p2p configuration
func DefaultConfig() Config {

	// DHTConfigValues defines default values for swarm config params.
	var DHTConfigValues = DiscoveryConfig{
		Gossip:                 false,
		Bootstrap:              false,
		RoutingTableBucketSize: 20,
		RoutingTableAlpha:      3,
		RandomConnections:      5,
		BootstrapNodes: 		[]string{ // these should be the spacemesh foundation bootstrap nodes
		},
	}

	return Config{
		TCPPort:         7513,
		NodeID:          "",
		NewNode:         false,
		DialTimeout:     duration("1m"),
		ConnKeepAlive:   duration("48h"),
		NetworkID:       TestNet,
		DiscoveryConfig: DHTConfigValues,
		BufferSize:      100,
	}
}
