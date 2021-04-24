// Package config defines configuration used in the p2p package
package config

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
)

const (
	// P2PDirectoryPath is the name of the directory where we store p2p stuff
	P2PDirectoryPath = "p2p"
	// NodeDataFileName is the name of the file we store the the p2p identity keys
	NodeDataFileName = "id.json"
	// NodesDirectoryName is the name of the directory we store nodes identities under
	NodesDirectoryName = "nodes"
	// UnlimitedMsgSize is a constant used to check whether message size is set to unlimited size
	UnlimitedMsgSize = 0
	// defaultTCPPort is the inet port that P2P listens on by default
	defaultTCPPort = 7513
	// defaultTCPInterface is the inet interface that P2P listens on by default
	defaultTCPInterface = "0.0.0.0"
)

// Values specifies default values for node config params.
var (
	Values = DefaultConfig()
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
	TCPInterface          string        `mapstructure:"tcp-interface"`
	AcquirePort           bool          `mapstructure:"acquire-port"`
	NodeID                string        `mapstructure:"node-id"`
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
	MsgSizeLimit          int           `mapstructure:"msg-size-limit"` // in bytes
}

// SwarmConfig specifies swarm config params.
type SwarmConfig struct {
	Gossip                 bool     `mapstructure:"gossip"`
	Bootstrap              bool     `mapstructure:"bootstrap"`
	RoutingTableBucketSize int      `mapstructure:"bucketsize"`
	RoutingTableAlpha      int      `mapstructure:"alpha"`
	RandomConnections      int      `mapstructure:"randcon"`
	BootstrapNodes         []string `mapstructure:"bootnodes"`
	PeersFile              string   `mapstructure:"peers-file"`
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
		BootstrapNodes:         []string{},   // these should be the spacemesh foundation bootstrap nodes
		PeersFile:              "peers.json", // located under data-dir/<publickey>/<peer-file> not loaded or save if empty string is given.
	}

	return Config{
		TCPPort:               defaultTCPPort,
		TCPInterface:          defaultTCPInterface,
		AcquirePort:           true,
		NodeID:                "",
		DialTimeout:           duration("1m"),
		ConnKeepAlive:         duration("48h"),
		NetworkID:             TestNet,
		ResponseTimeout:       duration("60s"),
		SessionTimeout:        duration("15s"),
		MaxPendingConnections: 100,
		OutboundPeersTarget:   10,
		MaxInboundPeers:       100,
		SwarmConfig:           SwarmConfigValues,
		BufferSize:            10000,
		MsgSizeLimit:          UnlimitedMsgSize,
	}
}

// DefaultTestConfig returns the default config for tests.
func DefaultTestConfig() Config {
	conf := DefaultConfig()
	conf.TCPPort += 10000
	conf.TCPInterface = "127.0.0.1"
	return conf
}
