package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct { // TODO(nkryuchkov): use unused fields
	ATXThreshold int    `mapstructure:"tortoise-beacon-atx-threshold"` // ATXs difficulty threshold in proposal message
	RoundsNumber uint64 `mapstructure:"tortoise-beacon-rounds-number"` // number of rounds
	WakeupDelta  int    `mapstructure:"tortoise-beacon-wakeup-delta"`  // the wakeup delta after tick
	Theta        int    `mapstructure:"tortoise-beacon-theta"`
	HDist        int    `mapstructure:"tortoise-beacon-hdist"` // TODO(nkryuchkov): consider using global hdist
	TAve         int    `mapstructure:"tortoise-beacon-t-ave"`
}

// DefaultConfig returns the default configuration for the tortoise beacon.
func DefaultConfig() Config {
	return Config{
		ATXThreshold: 50, // TODO(nkryuchkov): change
		RoundsNumber: 6,
		WakeupDelta:  30,
		Theta:        1, // TODO(nkryuchkov): change
		HDist:        20,
		TAve:         1, // TODO(nkryuchkov): change
	}
}

// TestConfig returns the test configuration for the tortoise beacon.
func TestConfig() Config {
	return Config{
		ATXThreshold: 50, // TODO(nkryuchkov): change
		RoundsNumber: 2,
		WakeupDelta:  1,
		Theta:        1,
		HDist:        1,
		TAve:         1,
	}
}
