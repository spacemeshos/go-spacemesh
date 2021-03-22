package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct {
	ATXThreshold   int    `mapstructure:"tortoise-beacon-atx-threshold"` // max allowed amount of ATXs in proposal message
	VotesNumber    int    `mapstructure:"tortoise-beacon-votes-number"`  // max allowed amount of votes in voting message
	BeaconDuration int    `mapstructure:"tortoise-beacon-duration"`      // Messages of the beacon are very large, we need to spread the sending of these messages, so that clients with slow connection can also participate. The solution is to run the protocol for a long enough period of time so that messages can be sent throughout this time
	RoundsNumber   uint64 `mapstructure:"tortoise-beacon-rounds-number"` // number of rounds
	WakeupDelta    int    `mapstructure:"tortoise-beacon-wakeup-delta"`  // the wakeup delta after tick
	Theta          int    `mapstructure:"tortoise-beacon-theta"`
	HDist          int    `mapstructure:"tortoise-beacon-hdist"`
	TAve           int    `mapstructure:"tortoise-beacon-t-ave"`
}

// DefaultConfig returns the default configuration for the tortoise beacon.
func DefaultConfig() Config {
	return Config{
		ATXThreshold:   50, // TODO(nkryuchkov): change
		VotesNumber:    50, // TODO(nkryuchkov): change
		BeaconDuration: 30, // TODO(nkryuchkov): change
		RoundsNumber:   6,
		WakeupDelta:    30,
		Theta:          1, // TODO(nkryuchkov): change
		HDist:          20,
		TAve:           1, // TODO(nkryuchkov): change
	}
}

// TestConfig returns the test configuration for the tortoise beacon.
func TestConfig() Config {
	return Config{
		ATXThreshold:   50, // TODO(nkryuchkov): change
		VotesNumber:    50, // TODO(nkryuchkov): change
		BeaconDuration: 30, // TODO(nkryuchkov): change
		RoundsNumber:   2,
		WakeupDelta:    1,
		Theta:          1,
		HDist:          1,
		TAve:           1,
	}
}
