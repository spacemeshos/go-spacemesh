package tortoisebeacon

// Config is the configuration of the Tortoise Beacon.
type Config struct {
	K           int `mapstructure:"tortoise-beacon-rounds-number"` // number of rounds
	WakeupDelta int `mapstructure:"tortoise-beacon-wakeup-delta"`  // the wakeup delta after tick
}

// DefaultConfig returns the default configuration for the tortoise beacon.
func DefaultConfig() Config {
	return Config{
		K:           6,
		WakeupDelta: 10,
	}
}

// TestConfig returns the test configuration for the tortoise beacon.
func TestConfig() Config {
	return Config{
		K:           2,
		WakeupDelta: 1,
	}
}
