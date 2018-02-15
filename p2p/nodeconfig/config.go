package nodeconfig

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"gopkg.in/urfave/cli.v1"
)

// ConfigValues specifies  default values for node config params.
var ConfigValues = Config{
	SecurityParam: 20,
	FastSync:      true,
	TCPPort:       7513,
	NodeID:        "",
	DialTimeout:   duration{"1m"},
	ConnKeepAlive: duration{"48h"},
	NetworkID:     int(TestNet),
	SwarmConfig:   SwarmConfigValues,
	TimeConfig:    TimeConfigValues,
}

// TimeConfigValues defines default values for all time and ntp related params.
var TimeConfigValues = TimeConfig{
	MaxAllowedDrift:       duration{"10s"},
	NtpQueries:            5,
	DefaultTimeoutLatency: duration{"30s"},
	RefreshNtpInterval:    duration{"30m"},
}

// SwarmConfigValues defines default values for swarm config params.
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
		log.Error("Could not parse duration string returning 0, error:", err)
	}
	return dur
}

// Config specifies node config params.
type Config struct {
	SecurityParam int
	FastSync      bool
	TCPPort       int
	NodeID        string
	DialTimeout   duration
	ConnKeepAlive duration
	NetworkID     int
	SwarmConfig   SwarmConfig
	TimeConfig    TimeConfig
}

// SwarmConfig specifies swarm config params.
type SwarmConfig struct {
	Bootstrap              bool
	RoutingTableBucketSize int
	RoutingTableAlpha      int
	RandomConnections      int
	BootstrapNodes         cli.StringSlice
}

// TimeConfig specifies the timesync params for ntp.
type TimeConfig struct {
	MaxAllowedDrift       duration
	NtpQueries            int
	DefaultTimeoutLatency duration
	RefreshNtpInterval    duration
}
