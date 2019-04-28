package config

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
)

// ConfigValues specifies  default values for node config params.
var (
	ConfigValues      = DefaultConfig()
	SwarmConfigValues = ConfigValues.SwarmConfig
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
	TCPPort               int           `mapstructure:"tcp-port"`
	NodeID                string        `mapstructure:"node-id"`
	NewNode               bool          `mapstructure:"new-node"`
	DialTimeout           time.Duration `mapstructure:"dial-timeout"`
	ConnKeepAlive         time.Duration `mapstructure:"conn-keepalive"`
	NetworkID             int8          `mapstructure:"network-id"`
	ResponseTimeout       time.Duration `mapstructure:"response-timeout"`
	SessionTimeout        time.Duration `mapstructure:"session-timeout"`
	MaxPendingConnections int           `mapstructure:"max-pending-connections"`
	OutboundPeersTarget   int           `mapstructure:"outbound-target"`
	MaxInboundPeers       int           `mapstructure:"max-inbound"`
	SwarmConfig           SwarmConfig   `mapstructure:"swarm"`
	BufferSize            int           `mapstructure:"buffer-size"`
}

// SwarmConfig specifies swarm config params.
type SwarmConfig struct {
	Gossip                 bool     `mapstructure:"gossip"`
	Bootstrap              bool     `mapstructure:"bootstrap"`
	RoutingTableBucketSize int      `mapstructure:"bucketsize"`
	RoutingTableAlpha      int      `mapstructure:"alpha"`
	RandomConnections      int      `mapstructure:"randcon"`
	BootstrapNodes         []string `mapstructure:"bootnodes"`
}

// DefaultConfig defines the default p2p configuration
func DefaultConfig() Config {

	// SwarmConfigValues defines default values for swarm config params.
	var SwarmConfigValues = SwarmConfig{
		Gossip:                 true,
		Bootstrap:              false,
		RoutingTableBucketSize: 20,
		RoutingTableAlpha:      3,
		RandomConnections:      5,
		BootstrapNodes:         []string{ // these should be the spacemesh foundation bootstrap nodes
		},
	}

	return Config{
		TCPPort:               7513,
		NodeID:                "",
		NewNode:               false,
		DialTimeout:           duration("1m"),
		ConnKeepAlive:         duration("48h"),
		NetworkID:             TestNet,
		ResponseTimeout:       duration("15s"),
		SessionTimeout:        duration("5s"),
		MaxPendingConnections: 100,
		OutboundPeersTarget:   10,
		MaxInboundPeers:       100,
		SwarmConfig:           SwarmConfigValues,
		BufferSize:            100,
	}
}
